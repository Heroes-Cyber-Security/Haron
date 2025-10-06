use crate::{
    eth::http,
    supervisor,
    types::RpcForwardResult,
};

use actix::{Actor, ActorContext, ActorFutureExt, AsyncContext, StreamHandler, WrapFuture};
use actix_web_actors::ws;
use anvil_rpc::{
    error::RpcError,
    request::Request as RpcRequest,
    response::Response as RpcResponseEnvelope,
};
use eyre::eyre;
use tokio::sync::{mpsc, oneshot};

pub struct JsonRpcWebsocketSession {
    node_id: String,
    sender: mpsc::Sender<supervisor::Command>,
}

impl JsonRpcWebsocketSession {
    pub fn new(node_id: String, sender: mpsc::Sender<supervisor::Command>) -> Self {
        Self { node_id, sender }
    }

    fn handle_json_text(&self, text: String, ctx: &mut ws::WebsocketContext<Self>) {
        match serde_json::from_str::<RpcRequest>(&text) {
            Ok(request) => self.forward_request(request, ctx),
            Err(err) => {
                eprintln!(
                    "invalid websocket json-rpc payload for {}: {}", self.node_id, err
                );
                Self::send_response(ctx, RpcResponseEnvelope::from(RpcError::parse_error()));
            }
        }
    }

    fn forward_request(&self, request: RpcRequest, ctx: &mut ws::WebsocketContext<Self>) {
        let sender = self.sender.clone();
        let node_id = self.node_id.clone();
        let fut = async move { dispatch_rpc_request(sender, request).await };

        ctx.spawn(
            fut.into_actor(self).map(move |result, _actor, ctx| match result {
                Ok(Some(response)) => Self::send_response(ctx, response),
                Ok(None) => (),
                Err(err) => {
                    eprintln!("websocket rpc forward error for {node_id}: {err}");
                    Self::send_response(ctx, err.into_response());
                }
            }),
        );
    }

    fn send_response(ctx: &mut ws::WebsocketContext<Self>, response: RpcResponseEnvelope) {
        match serde_json::to_string(&response) {
            Ok(payload) => ctx.text(payload),
            Err(err) => {
                eprintln!("failed to serialize websocket rpc response: {err}");
            }
        }
    }
}

impl Actor for JsonRpcWebsocketSession {
    type Context = ws::WebsocketContext<Self>;
}

impl StreamHandler<Result<ws::Message, ws::ProtocolError>> for JsonRpcWebsocketSession {
    fn handle(&mut self, item: Result<ws::Message, ws::ProtocolError>, ctx: &mut Self::Context) {
        match item {
            Ok(ws::Message::Text(text)) => self.handle_json_text(text.to_string(), ctx),
            Ok(ws::Message::Binary(bin)) => match String::from_utf8(bin.to_vec()) {
                Ok(text) => self.handle_json_text(text, ctx),
                Err(err) => {
                    eprintln!(
                        "binary websocket payload is not valid utf-8 for {}: {err}",
                        self.node_id
                    );
                    Self::send_response(ctx, RpcResponseEnvelope::from(RpcError::parse_error()));
                }
            },
            Ok(ws::Message::Ping(payload)) => {
                ctx.pong(&payload);
            }
            Ok(ws::Message::Pong(_)) => {}
            Ok(ws::Message::Close(reason)) => {
                ctx.close(reason);
                ctx.stop();
            }
            Ok(ws::Message::Continuation(_)) => {
                ctx.stop();
            }
            Ok(ws::Message::Nop) => {}
            Err(err) => {
                eprintln!("websocket protocol error for {}: {err}", self.node_id);
                ctx.stop();
            }
        }
    }
}

#[derive(Debug)]
enum ForwardError {
    SupervisorUnavailable,
    ExecutionFailed(String),
    MissingResponse,
}

impl std::fmt::Display for ForwardError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            Self::SupervisorUnavailable => write!(f, "supervisor unavailable"),
            Self::ExecutionFailed(reason) => write!(f, "execution failed: {reason}"),
            Self::MissingResponse => write!(f, "missing rpc response"),
        }
    }
}

impl ForwardError {
    fn into_response(self) -> RpcResponseEnvelope {
        let error = match self {
            Self::SupervisorUnavailable => {
                RpcError::internal_error_with("supervisor unavailable for websocket call")
            }
            Self::ExecutionFailed(reason) => {
                RpcError::internal_error_with(format!("execution against anvil failed: {reason}"))
            }
            Self::MissingResponse => {
                RpcError::internal_error_with("missing response from supervisor")
            }
        };
        RpcResponseEnvelope::from(error)
    }
}

impl std::error::Error for ForwardError {}

async fn dispatch_rpc_request(
    sender: mpsc::Sender<supervisor::Command>,
    request: RpcRequest,
) -> Result<RpcForwardResult, ForwardError> {
    let (response_tx, response_rx) = oneshot::channel();
    let job_request = request.clone();
    let job = Box::new(move |api: crate::anvil::EthApi| -> eyre::Result<()> {
        let handle = tokio::runtime::Handle::current();
        let response = http::execute_rpc_request(&handle, &api, job_request);
        response_tx
            .send(response)
            .map_err(|_| eyre!("websocket layer dropped response channel"))?;
        Ok(())
    });

    let (ack_tx, ack_rx) = oneshot::channel();
    sender
        .send(supervisor::Command::Execute { job, respond_to: Some(ack_tx) })
        .await
        .map_err(|_| ForwardError::SupervisorUnavailable)?;

    match ack_rx.await {
        Ok(Ok(())) => {}
        Ok(Err(err)) => return Err(ForwardError::ExecutionFailed(err.to_string())),
        Err(_) => return Err(ForwardError::SupervisorUnavailable),
    }

    response_rx.await.map_err(|_| ForwardError::MissingResponse)
}
