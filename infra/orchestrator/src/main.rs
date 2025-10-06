mod anvil;
mod supervisor;

use actix_web::{App, HttpResponse, HttpServer, Responder, post, web};
use anvil_core::eth::EthRequest;
use anvil_rpc::{
    error::RpcError,
    request::{Request as RpcRequest, RequestParams, RpcCall, RpcMethodCall, RpcNotification},
    response::{Response as RpcResponseEnvelope, ResponseResult, RpcResponse},
};
use eyre::{Result as EyreResult, eyre};
use serde_json::{Value, json};
use std::collections::HashMap;
use tokio::{
    sync::{Mutex, mpsc, oneshot},
    task::JoinHandle,
};

struct AppState {
    nodes: Mutex<HashMap<String, NodeEntry>>,
}

impl AppState {
    fn new() -> Self {
        Self {
            nodes: Mutex::new(HashMap::new()),
        }
    }
}

struct NodeEntry {
    api: anvil::EthApi,
    sender: mpsc::Sender<supervisor::Command>,
    task: JoinHandle<()>,
}

impl NodeEntry {
    fn new(
        api: anvil::EthApi,
        sender: mpsc::Sender<supervisor::Command>,
        task: JoinHandle<()>,
    ) -> Self {
        Self { api, sender, task }
    }

    async fn shutdown(self) {
        let NodeEntry { api, sender, task } = self;
        drop(api);
        let (tx, rx) = oneshot::channel();
        let _ = sender
            .send(supervisor::Command::Shutdown {
                respond_to: Some(tx),
            })
            .await;
        let _ = rx.await;
        let _ = task.await;
    }
}

async fn spawn_supervised_node(config: anvil::NodeConfig) -> EyreResult<NodeEntry> {
    let (command_tx, command_rx) = mpsc::channel(8);
    let supervisor_task = supervisor::spawn(command_rx, config);

    let (api_tx, api_rx) = oneshot::channel();
    let (result_tx, result_rx) = oneshot::channel();
    let job = Box::new(move |api: anvil::EthApi| -> EyreResult<()> {
        let api_clone = api.clone();
        api_tx
            .send(api_clone)
            .map_err(|_| eyre!("failed to deliver api handle to caller"))?;
        Ok(())
    });

    command_tx
        .send(supervisor::Command::Execute {
            job,
            respond_to: Some(result_tx),
        })
        .await
        .map_err(|_| eyre!("supervisor command channel closed before initialization"))?;

    match result_rx.await {
        Ok(Ok(())) => {}
        Ok(Err(err)) => return Err(err),
        Err(_) => return Err(eyre!("failed to receive initialization confirmation")),
    }

    let api = api_rx
        .await
        .map_err(|_| eyre!("failed to receive api handle from supervisor"))?;

    Ok(NodeEntry::new(api, command_tx, supervisor_task))
}

#[post("/deploy/{id}")]
async fn service_deploy(state: web::Data<AppState>, params: web::Path<String>) -> impl Responder {
    let id = params.into_inner();

    {
        let nodes = state.nodes.lock().await;
        if nodes.contains_key(&id) {
            return "ALREADY_EXISTS";
        }
    }

    let config = anvil::NodeConfig::default();
    let entry = match spawn_supervised_node(config).await {
        Ok(entry) => entry,
        Err(err) => {
            eprintln!("failed to spawn supervised anvil: {err:?}");
            return "ERR";
        }
    };

    let mut nodes = state.nodes.lock().await;
    if nodes.contains_key(&id) {
        drop(nodes);

        entry.shutdown().await;
        trim_allocator();

        return "ALREADY_EXISTS";
    }

    nodes.insert(id, entry);
    drop(nodes);

    "OK"
}

#[post("/stop/{id}")]
async fn stop_node(state: web::Data<AppState>, params: web::Path<String>) -> impl Responder {
    let id = params.into_inner();

    let entry = {
        let mut nodes = state.nodes.lock().await;
        let entry = nodes.remove(&id);
        if entry.is_some() {
            nodes.shrink_to_fit();
        }
        entry
    };

    match entry {
        Some(entry) => {
            entry.shutdown().await;
            trim_allocator();
            "OK"
        }
        None => "NOT_FOUND",
    }
}

#[post("/anvil/{id}")]
async fn forward_eth_json_rpc(
    state: web::Data<AppState>,
    params: web::Path<String>,
    body: web::Json<Value>,
) -> Result<HttpResponse, actix_web::Error> {
    let id = params.into_inner();
    let rpc_request: RpcRequest = match serde_json::from_value(body.into_inner()) {
        Ok(request) => request,
        Err(err) => {
            eprintln!("invalid json-rpc payload for {id}: {err}");
            return Ok(HttpResponse::BadRequest().body("INVALID_JSON_RPC"));
        }
    };

    let sender = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node) => node.sender.clone(),
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    let (response_tx, response_rx) = oneshot::channel();
    let job_request = rpc_request.clone();
    let job = Box::new(move |api: anvil::EthApi| -> EyreResult<()> {
        let response = process_rpc_request(api, job_request);
        response_tx
            .send(response)
            .map_err(|_| eyre!("http layer dropped response channel"))?;
        Ok(())
    });

    let (ack_tx, ack_rx) = oneshot::channel();
    sender
        .send(supervisor::Command::Execute {
            job,
            respond_to: Some(ack_tx),
        })
        .await
        .map_err(|_| actix_web::error::ErrorInternalServerError("SUPERVISOR_UNAVAILABLE"))?;

    match ack_rx.await {
        Ok(Ok(())) => {}
        Ok(Err(err)) => {
            eprintln!("supervisor execution error for {id}: {err:?}");
            return Ok(HttpResponse::InternalServerError().body("RPC_EXECUTION_FAILED"));
        }
        Err(_) => return Ok(HttpResponse::InternalServerError().body("SUPERVISOR_DROPPED")),
    }

    let response = response_rx
        .await
        .map_err(|_| actix_web::error::ErrorInternalServerError("MISSING_RPC_RESPONSE"))?;

    match response {
        Some(payload) => Ok(HttpResponse::Ok().json(payload)),
        None => Ok(HttpResponse::NoContent().finish()),
    }
}

type RpcForwardResult = Option<RpcResponseEnvelope>;

fn process_rpc_request(api: anvil::EthApi, request: RpcRequest) -> RpcForwardResult {
    let handle = tokio::runtime::Handle::current();
    match request {
        RpcRequest::Single(call) => {
            handle_rpc_call(&handle, &api, call).map(RpcResponseEnvelope::Single)
        }
        RpcRequest::Batch(calls) => {
            let mut responses = Vec::with_capacity(calls.len());
            for call in calls {
                if let Some(response) = handle_rpc_call(&handle, &api, call) {
                    responses.push(response);
                }
            }
            if responses.is_empty() {
                None
            } else {
                Some(RpcResponseEnvelope::Batch(responses))
            }
        }
    }
}

fn handle_rpc_call(
    handle: &tokio::runtime::Handle,
    api: &anvil::EthApi,
    call: RpcCall,
) -> Option<RpcResponse> {
    match call {
        RpcCall::MethodCall(method_call) => Some(handle_method_call(handle, api, method_call)),
        RpcCall::Notification(notification) => {
            handle_notification(handle, api, notification);
            None
        }
        RpcCall::Invalid { id } => Some(RpcResponse::invalid_request(id)),
    }
}

fn handle_method_call(
    handle: &tokio::runtime::Handle,
    api: &anvil::EthApi,
    call: RpcMethodCall,
) -> RpcResponse {
    let RpcMethodCall {
        method, params, id, ..
    } = call;
    match build_eth_request(method, params) {
        Ok(eth_request) => {
            let result = handle.block_on(api.execute(eth_request));
            RpcResponse::new(id, result)
        }
        Err(err) => {
            let rpc_error = map_serde_error(err);
            RpcResponse::new(id, ResponseResult::Error(rpc_error))
        }
    }
}

fn handle_notification(
    handle: &tokio::runtime::Handle,
    api: &anvil::EthApi,
    notification: RpcNotification,
) {
    let RpcNotification { method, params, .. } = notification;
    match build_eth_request(method, params) {
        Ok(eth_request) => {
            let _ = handle.block_on(api.execute(eth_request));
        }
        Err(err) => {
            eprintln!("failed to parse json-rpc notification: {err}");
        }
    }
}

fn build_eth_request(
    method: String,
    params: RequestParams,
) -> Result<EthRequest, serde_json::Error> {
    let params_value: Value = params.into();
    let value = json!({ "method": method, "params": params_value });
    serde_json::from_value(value)
}

fn map_serde_error(err: serde_json::Error) -> RpcError {
    use serde_json::error::Category;
    match err.classify() {
        Category::Data if err.to_string().contains("unknown variant") => {
            RpcError::method_not_found()
        }
        Category::Data => RpcError::invalid_params(err.to_string()),
        Category::Syntax => RpcError::parse_error(),
        Category::Io | Category::Eof => RpcError::internal_error(),
    }
}

fn trim_allocator() {
    #[cfg(target_os = "linux")]
    unsafe {
        libc::malloc_trim(0);
    }
}

#[actix_web::main]
async fn main() -> std::io::Result<()> {
    let state = web::Data::new(AppState::new());

    HttpServer::new(move || {
        App::new()
            .app_data(state.clone())
            .service(service_deploy)
            .service(stop_node)
            .service(forward_eth_json_rpc)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
