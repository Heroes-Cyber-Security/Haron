use crate::{
    eth::http,
    supervisor::{self, SubscriptionRequest, SubscriptionSetup},
    types::RpcForwardResult,
};

use actix::{
    Actor, ActorContext, ActorFutureExt, AsyncContext, Handler, Message, StreamHandler, WrapFuture,
};
use actix_web_actors::ws;
use anvil_core::eth::EthPubSub;
use anvil_core::eth::subscription::SubscriptionId;
use anvil_rpc::{
    error::RpcError,
    request::{Id as RpcId, Request as RpcRequest, RpcCall, RpcMethodCall},
    response::{Response as RpcResponseEnvelope, ResponseResult, RpcResponse},
};
use eyre::eyre;
use futures_util::StreamExt;
use serde_json::{Value, json};
use std::collections::HashMap;
use tokio::{
    sync::{mpsc, oneshot},
    task::JoinHandle,
};

pub struct JsonRpcWebsocketSession {
    node_id: String,
    sender: mpsc::Sender<supervisor::Command>,
    subscriptions: HashMap<SubscriptionId, ActiveSubscription>,
}

impl JsonRpcWebsocketSession {
    pub fn new(node_id: String, sender: mpsc::Sender<supervisor::Command>) -> Self {
        Self {
            node_id,
            sender,
            subscriptions: HashMap::new(),
        }
    }

    fn handle_json_text(&mut self, text: String, ctx: &mut ws::WebsocketContext<Self>) {
        match serde_json::from_str::<RpcRequest>(&text) {
            Ok(request) => self.process_rpc_request(request, ctx),
            Err(err) => {
                eprintln!(
                    "invalid websocket json-rpc payload for {}: {}",
                    self.node_id, err
                );
                Self::send_response(ctx, RpcResponseEnvelope::from(RpcError::parse_error()));
            }
        }
    }

    fn process_rpc_request(&mut self, request: RpcRequest, ctx: &mut ws::WebsocketContext<Self>) {
        match request {
            RpcRequest::Single(call) => self.handle_rpc_call(call, ctx),
            RpcRequest::Batch(_) => self.forward_standard_request(request, ctx),
        }
    }

    fn handle_rpc_call(&mut self, call: RpcCall, ctx: &mut ws::WebsocketContext<Self>) {
        match call {
            RpcCall::MethodCall(method_call) => match method_call.method.as_str() {
                "eth_subscribe" => self.handle_eth_subscribe(method_call, ctx),
                "eth_unsubscribe" => self.handle_eth_unsubscribe(method_call, ctx),
                _ => self.forward_standard_request(
                    RpcRequest::Single(RpcCall::MethodCall(method_call)),
                    ctx,
                ),
            },
            RpcCall::Notification(notification) => {
                self.forward_standard_request(
                    RpcRequest::Single(RpcCall::Notification(notification)),
                    ctx,
                );
            }
            RpcCall::Invalid { id } => {
                Self::send_response(
                    ctx,
                    RpcResponseEnvelope::Single(RpcResponse::invalid_request(id)),
                );
            }
        }
    }

    fn forward_standard_request(&self, request: RpcRequest, ctx: &mut ws::WebsocketContext<Self>) {
        let sender = self.sender.clone();
        let node_id = self.node_id.clone();
        let fut = async move { dispatch_rpc_request(sender, request).await };

        ctx.spawn(
            fut.into_actor(self)
                .map(move |result, _actor, ctx| match result {
                    Ok(Some(response)) => Self::send_response(ctx, response),
                    Ok(None) => {}
                    Err(err) => {
                        eprintln!("websocket rpc forward error for {node_id}: {err}");
                        Self::send_response(ctx, err.into_response());
                    }
                }),
        );
    }

    fn handle_eth_subscribe(&mut self, call: RpcMethodCall, ctx: &mut ws::WebsocketContext<Self>) {
        let request_id = call.id.clone();
        match parse_pubsub_call(&call) {
            Ok(EthPubSub::EthSubscribe(kind, params)) => {
                let request = SubscriptionRequest { kind, params };
                let sender = self.sender.clone();
                let fut = async move { subscribe_via_supervisor(sender, request).await };
                ctx.spawn(
                    fut.into_actor(self)
                        .map(move |result, actor, ctx| match result {
                            Ok(setup) => {
                                actor.register_subscription(setup, request_id.clone(), ctx)
                            }
                            Err(err) => {
                                let response = err.into_response(request_id.clone());
                                Self::send_response(ctx, response);
                            }
                        }),
                );
            }
            Ok(_) => {
                let response = RpcResponse::new(
                    request_id.clone(),
                    ResponseResult::Error(RpcError::method_not_found()),
                );
                Self::send_response(ctx, RpcResponseEnvelope::Single(response));
            }
            Err(err) => {
                eprintln!("failed to parse eth_subscribe payload: {err}");
                let response = RpcResponse::new(
                    request_id.clone(),
                    ResponseResult::Error(RpcError::invalid_params(err.to_string())),
                );
                Self::send_response(ctx, RpcResponseEnvelope::Single(response));
            }
        }
    }

    fn handle_eth_unsubscribe(
        &mut self,
        call: RpcMethodCall,
        ctx: &mut ws::WebsocketContext<Self>,
    ) {
        let request_id = call.id.clone();
        match parse_pubsub_call(&call) {
            Ok(EthPubSub::EthUnSubscribe(subscription_id)) => {
                let removed = self.remove_subscription(&subscription_id);
                let response = RpcResponse::new(request_id, ResponseResult::success(removed));
                Self::send_response(ctx, RpcResponseEnvelope::Single(response));
            }
            Ok(_) => {
                let response = RpcResponse::new(
                    request_id,
                    ResponseResult::Error(RpcError::method_not_found()),
                );
                Self::send_response(ctx, RpcResponseEnvelope::Single(response));
            }
            Err(err) => {
                eprintln!("failed to parse eth_unsubscribe payload: {err}");
                let response = RpcResponse::new(
                    request_id,
                    ResponseResult::Error(RpcError::invalid_params(err.to_string())),
                );
                Self::send_response(ctx, RpcResponseEnvelope::Single(response));
            }
        }
    }

    fn register_subscription(
        &mut self,
        setup: SubscriptionSetup,
        request_id: RpcId,
        ctx: &mut ws::WebsocketContext<Self>,
    ) {
        let SubscriptionSetup { id, mut stream } = setup;
        let addr = ctx.address();
        let task = tokio::spawn(async move {
            while let Some(payload) = stream.next().await {
                if addr.try_send(SubscriptionNotification { payload }).is_err() {
                    break;
                }
            }
        });

        if let Some(existing) = self
            .subscriptions
            .insert(id.clone(), ActiveSubscription { task })
        {
            existing.abort();
        }

        let response = RpcResponse::new(request_id, ResponseResult::success(id));
        Self::send_response(ctx, RpcResponseEnvelope::Single(response));
    }

    fn remove_subscription(&mut self, id: &SubscriptionId) -> bool {
        self.subscriptions
            .remove(id)
            .map(|subscription| {
                subscription.abort();
                true
            })
            .unwrap_or(false)
    }

    fn send_response(ctx: &mut ws::WebsocketContext<Self>, response: RpcResponseEnvelope) {
        match serde_json::to_string(&response) {
            Ok(payload) => ctx.text(payload),
            Err(err) => {
                eprintln!("failed to serialize websocket rpc response: {err}");
            }
        }
    }

    fn clear_subscriptions(&mut self) {
        for (_, subscription) in self.subscriptions.drain() {
            subscription.abort();
        }
    }
}

impl Actor for JsonRpcWebsocketSession {
    type Context = ws::WebsocketContext<Self>;

    fn stopped(&mut self, _ctx: &mut Self::Context) {
        self.clear_subscriptions();
    }
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

#[derive(Message)]
#[rtype(result = "()")]
struct SubscriptionNotification {
    payload: Value,
}

impl Handler<SubscriptionNotification> for JsonRpcWebsocketSession {
    type Result = ();

    fn handle(&mut self, msg: SubscriptionNotification, ctx: &mut Self::Context) {
        match serde_json::to_string(&msg.payload) {
            Ok(payload) => ctx.text(payload),
            Err(err) => {
                eprintln!("failed to serialize subscription notification: {err}");
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
        .send(supervisor::Command::Execute {
            job,
            respond_to: Some(ack_tx),
        })
        .await
        .map_err(|_| ForwardError::SupervisorUnavailable)?;

    match ack_rx.await {
        Ok(Ok(())) => {}
        Ok(Err(err)) => return Err(ForwardError::ExecutionFailed(err.to_string())),
        Err(_) => return Err(ForwardError::SupervisorUnavailable),
    }

    response_rx.await.map_err(|_| ForwardError::MissingResponse)
}

#[derive(Debug)]
enum SubscribeError {
    SupervisorUnavailable,
    MissingResponse,
    Failed(RpcError),
}

impl SubscribeError {
    fn into_response(self, request_id: RpcId) -> RpcResponseEnvelope {
        let error = match self {
            SubscribeError::SupervisorUnavailable => {
                RpcError::internal_error_with("supervisor unavailable for subscription")
            }
            SubscribeError::MissingResponse => {
                RpcError::internal_error_with("missing subscription response from supervisor")
            }
            SubscribeError::Failed(err) => err,
        };
        RpcResponseEnvelope::Single(RpcResponse::new(request_id, ResponseResult::Error(error)))
    }
}

fn parse_pubsub_call(call: &RpcMethodCall) -> Result<EthPubSub, serde_json::Error> {
    let params_value = serde_json::Value::from(call.params.clone());
    let value = json!({
        "method": call.method.clone(),
        "params": params_value,
    });
    serde_json::from_value(value)
}

async fn subscribe_via_supervisor(
    sender: mpsc::Sender<supervisor::Command>,
    request: SubscriptionRequest,
) -> Result<SubscriptionSetup, SubscribeError> {
    let (response_tx, response_rx) = oneshot::channel();
    sender
        .send(supervisor::Command::Subscribe {
            request,
            respond_to: response_tx,
        })
        .await
        .map_err(|_| SubscribeError::SupervisorUnavailable)?;

    match response_rx
        .await
        .map_err(|_| SubscribeError::MissingResponse)?
    {
        Ok(setup) => Ok(setup),
        Err(err) => Err(SubscribeError::Failed(err)),
    }
}

struct ActiveSubscription {
    task: JoinHandle<()>,
}

impl ActiveSubscription {
    fn abort(self) {
        self.task.abort();
    }
}
