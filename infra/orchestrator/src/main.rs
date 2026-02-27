mod anvil;
mod eth;
mod supervisor;
mod types;

use eth::{http as eth_http, ws as eth_ws};
use types::{NodeEntry, SingleNodeEntry};

use actix_web::{App, HttpRequest, HttpResponse, HttpServer, Responder, get, post, web};
use anvil_rpc::request::Request as RpcRequest;
use eyre::{Result as EyreResult, eyre};
use serde::Deserialize;
use serde_json::Value;
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

async fn spawn_supervised_node(config: anvil::NodeConfig, chain_id: u64) -> EyreResult<SingleNodeEntry> {
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

    Ok(SingleNodeEntry { api, sender: command_tx, task: supervisor_task, chain_id })
}

#[derive(serde::Deserialize)]
struct DeployQuery {
    chains: Option<String>,
}

#[post("/deploy/{id}")]
async fn service_deploy(
    state: web::Data<AppState>,
    params: web::Path<String>,
    query: web::Query<DeployQuery>,
) -> impl Responder {
    let id = params.into_inner();

    {
        let nodes = state.nodes.lock().await;
        if nodes.contains_key(&id) {
            return "ALREADY_EXISTS";
        }
    }

    let chain_ids: Vec<u64> = match query.chains.as_deref() {
        Some(chains_str) => {
            let mut ids: Vec<u64> = chains_str
                .split(',')
                .filter_map(|s| s.trim().parse().ok())
                .collect();
            if ids.is_empty() {
                vec![1]
            } else {
                ids
            }
        }
        None => vec![1],
    };

    let mut unique_check = std::collections::HashSet::new();
    for id in &chain_ids {
        if !unique_check.insert(*id) {
            eprintln!("duplicate chainId detected: {id}");
            return "DUPLICATE_CHAIN_ID";
        }
    }

    let mut deployed_nodes: std::collections::HashMap<u64, SingleNodeEntry> =
        std::collections::HashMap::new();

    for chain_id in &chain_ids {
        let config = anvil::create_config(*chain_id);
        match spawn_supervised_node(config, *chain_id).await {
            Ok(entry) => {
                deployed_nodes.insert(*chain_id, entry);
            }
            Err(err) => {
                eprintln!("failed to spawn supervised anvil for chain {chain_id}: {err:?}");
                for (_, node) in deployed_nodes {
                    node.shutdown().await;
                }
                trim_allocator();
                return "ERR";
            }
        }
    }

    let mut nodes = state.nodes.lock().await;
    if nodes.contains_key(&id) {
        drop(nodes);
        for (_, node) in deployed_nodes {
            node.shutdown().await;
        }
        trim_allocator();
        return "ALREADY_EXISTS";
    }

    nodes.insert(id, NodeEntry { nodes: deployed_nodes });
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
async fn forward_eth_json_rpc_compat(
    state: web::Data<AppState>,
    params: web::Path<String>,
    body: web::Json<Value>,
) -> Result<HttpResponse, actix_web::Error> {
    let id = params.into_inner();

    let sender = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => {
                let first_chain = node_entry.nodes.keys().min().copied();
                match first_chain {
                    Some(chain_id) => {
                        match node_entry.nodes.get(&chain_id) {
                            Some(node) => node.sender.clone(),
                            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                        }
                    }
                    None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                }
            }
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    let rpc_request: RpcRequest = match serde_json::from_value(body.into_inner()) {
        Ok(request) => request,
        Err(err) => {
            eprintln!("invalid json-rpc payload for {id}: {err}");
            return Ok(HttpResponse::BadRequest().body("INVALID_JSON_RPC"));
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

#[post("/anvil/{id}/{chain_id}")]
async fn forward_eth_json_rpc(
    state: web::Data<AppState>,
    params: web::Path<(String, u64)>,
    body: web::Json<Value>,
) -> Result<HttpResponse, actix_web::Error> {
    let (id, chain_id) = params.into_inner();
    let rpc_request: RpcRequest = match serde_json::from_value(body.into_inner()) {
        Ok(request) => request,
        Err(err) => {
            eprintln!("invalid json-rpc payload for {id}/{chain_id}: {err}");
            return Ok(HttpResponse::BadRequest().body("INVALID_JSON_RPC"));
        }
    };

    let sender = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => match node_entry.nodes.get(&chain_id) {
                Some(node) => node.sender.clone(),
                None => return Ok(HttpResponse::NotFound().body("CHAIN_NOT_FOUND")),
            },
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
            eprintln!("supervisor execution error for {id}/{chain_id}: {err:?}");
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

#[get("/anvil/{id}")]
async fn forward_eth_json_rpc_ws_compat(
    state: web::Data<AppState>,
    params: web::Path<String>,
    req: HttpRequest,
    payload: web::Payload,
) -> Result<HttpResponse, actix_web::Error> {
    let id = params.into_inner();

    let sender = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => {
                let first_chain = node_entry.nodes.keys().min().copied();
                match first_chain {
                    Some(chain_id) => {
                        match node_entry.nodes.get(&chain_id) {
                            Some(node) => node.sender.clone(),
                            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                        }
                    }
                    None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                }
            }
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    eth_ws::start_json_rpc_websocket(&req, payload, id, sender)
}

#[get("/anvil/{id}/{chain_id}")]
async fn forward_eth_json_rpc_ws(
    state: web::Data<AppState>,
    params: web::Path<(String, u64)>,
    req: HttpRequest,
    payload: web::Payload,
) -> Result<HttpResponse, actix_web::Error> {
    let (id, chain_id) = params.into_inner();

    let sender = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => match node_entry.nodes.get(&chain_id) {
                Some(node) => node.sender.clone(),
                None => return Ok(HttpResponse::NotFound().body("CHAIN_NOT_FOUND")),
            },
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    eth_ws::start_json_rpc_websocket(&req, payload, format!("{id}/{chain_id}"), sender)
}

fn process_rpc_request(api: anvil::EthApi, request: RpcRequest) -> types::RpcForwardResult {
    let handle = tokio::runtime::Handle::current();
    eth_http::execute_rpc_request(&handle, &api, request)
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
            .service(forward_eth_json_rpc_compat)
            .service(forward_eth_json_rpc_ws)
            .service(forward_eth_json_rpc_ws_compat)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
