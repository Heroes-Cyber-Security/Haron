mod anvil;
mod eth;
mod hooks;
mod supervisor;
mod types;

use eth::{http as eth_http, ws as eth_ws};
use types::{NodeEntry, SingleNodeEntry};

use actix_web::{App, HttpRequest, HttpResponse, HttpServer, Responder, get, post, web};
use anvil_rpc::request::Request as RpcRequest;
use eyre::{Result as EyreResult, eyre};
use serde_json::Value;
use std::collections::HashMap;
use tokio::sync::{Mutex, mpsc, oneshot};

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

async fn spawn_supervised_node(
    config: anvil::NodeConfig,
    chain_id: u64,
) -> EyreResult<SingleNodeEntry> {
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

    Ok(SingleNodeEntry {
        api,
        sender: command_tx,
        task: supervisor_task,
        chain_id,
    })
}

#[derive(serde::Deserialize)]
struct DeployQuery {
    chains: Option<String>,
    block_times: Option<String>,
    tx_orders: Option<String>,
    /// Challenge hash. When present the orchestrator asks the worker for the
    /// challenge's deploy-time hooks (genesis, precompiles, firewall).
    challenge: Option<String>,
}

/// Splits a comma separated query parameter into trimmed, non-empty values.
fn split_csv(value: Option<&str>) -> Vec<String> {
    match value {
        Some(raw) => raw
            .split(',')
            .map(|part| part.trim().to_string())
            .filter(|part| !part.is_empty())
            .collect(),
        None => Vec::new(),
    }
}

/// Parses the `chains` query parameter into chain ids, defaulting to chain 1.
fn parse_chain_ids(chains: Option<&str>) -> Vec<u64> {
    let ids: Vec<u64> = chains
        .unwrap_or("")
        .split(',')
        .filter_map(|part| part.trim().parse().ok())
        .collect();

    if ids.is_empty() { vec![1] } else { ids }
}

/// Resolves the per-chain node parameters for a deploy request.
///
/// `block_times` and `tx_orders` are parallel to `chains`. Entries that are
/// missing, shorter than `chains`, or unparseable fall back to the defaults
/// instead of failing the deploy.
fn resolve_chain_params(
    chain_ids: &[u64],
    block_times: Option<&str>,
    tx_orders: Option<&str>,
) -> Vec<(u64, Option<String>)> {
    let block_times = split_csv(block_times);
    let tx_orders = split_csv(tx_orders);

    chain_ids
        .iter()
        .enumerate()
        .map(|(index, _)| {
            let block_time = block_times
                .get(index)
                .and_then(|value| value.parse::<u64>().ok())
                .unwrap_or(anvil::DEFAULT_BLOCK_TIME);
            (block_time, tx_orders.get(index).cloned())
        })
        .collect()
}

#[post("/deploy/{id}")]
async fn service_deploy(
    state: web::Data<AppState>,
    params: web::Path<String>,
    query: web::Query<DeployQuery>,
) -> impl Responder {
    let id = params.into_inner();

    let chain_ids: Vec<u64> = parse_chain_ids(query.chains.as_deref());

    let mut unique_check = std::collections::HashSet::new();
    for chain_id in &chain_ids {
        if !unique_check.insert(*chain_id) {
            eprintln!("duplicate chainId detected: {chain_id}");
            return "DUPLICATE_CHAIN_ID";
        }
    }

    let chain_params = resolve_chain_params(
        &chain_ids,
        query.block_times.as_deref(),
        query.tx_orders.as_deref(),
    );

    // Reject duplicates early, but never hold the global node lock across the
    // worker round trip: other instances' RPC would stall behind it.
    {
        let nodes = state.nodes.lock().await;
        if nodes.contains_key(&id) {
            return "ALREADY_EXISTS";
        }
    }

    let hooks = match query.challenge.as_deref() {
        Some(challenge) if !challenge.is_empty() => {
            hooks::fetch(&hooks::worker_base(), challenge, &id, &chain_ids).await
        }
        _ => None,
    };

    let mut deployed_nodes: std::collections::HashMap<u64, SingleNodeEntry> =
        std::collections::HashMap::new();

    for (chain_id, (block_time, tx_order)) in chain_ids.iter().zip(chain_params) {
        let config =
            anvil::create_config(*chain_id, block_time, tx_order.as_deref(), hooks.as_ref());
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

    // The screener exists from the start but stays inert until `/arm/{id}`, so
    // the challenge's own setup transactions are never filtered.
    let screener = hooks
        .as_ref()
        .filter(|hooks| hooks.incoming_tx)
        .and_then(|hooks| hooks::Screener::new(hooks, Default::default()));

    let mut nodes = state.nodes.lock().await;
    if nodes.contains_key(&id) {
        drop(nodes);
        for (_, node) in deployed_nodes {
            node.shutdown().await;
        }
        trim_allocator();
        return "ALREADY_EXISTS";
    }

    nodes.insert(
        id,
        NodeEntry {
            nodes: deployed_nodes,
            screener,
        },
    );

    "OK"
}

/// Arms the transaction firewall for an instance.
///
/// The worker calls this once `chal.py`'s `start()` has deployed the challenge,
/// so the setup transactions are exempt while everything the player sends is
/// screened.
#[post("/arm/{id}")]
async fn arm_node(state: web::Data<AppState>, params: web::Path<String>) -> impl Responder {
    let id = params.into_inner();

    let nodes = state.nodes.lock().await;
    match nodes.get(&id).and_then(|entry| entry.screener.as_ref()) {
        Some(screener) => {
            screener.arm();
            "OK"
        }
        None => "NO_HOOKS",
    }
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

    let (sender, chain_id, screener) = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => {
                let first_chain = node_entry.nodes.keys().min().copied();
                match first_chain {
                    Some(chain_id) => match node_entry.nodes.get(&chain_id) {
                        Some(node) => (node.sender.clone(), chain_id, node_entry.screener.clone()),
                        None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                    },
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

    let screened = hooks::apply(screener.as_ref(), chain_id, rpc_request).await;
    let Some(forward) = screened.forward else {
        return Ok(rpc_http_response(hooks::merge(
            None,
            screened.denied,
            screened.was_single,
        )));
    };

    let (response_tx, response_rx) = oneshot::channel();
    let job = Box::new(move |api: anvil::EthApi| -> EyreResult<()> {
        let response = process_rpc_request(api, forward);
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

    Ok(rpc_http_response(hooks::merge(
        response,
        screened.denied,
        screened.was_single,
    )))
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

    let (sender, screener) = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => match node_entry.nodes.get(&chain_id) {
                Some(node) => (node.sender.clone(), node_entry.screener.clone()),
                None => return Ok(HttpResponse::NotFound().body("CHAIN_NOT_FOUND")),
            },
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    let screened = hooks::apply(screener.as_ref(), chain_id, rpc_request).await;
    let Some(forward) = screened.forward else {
        return Ok(rpc_http_response(hooks::merge(
            None,
            screened.denied,
            screened.was_single,
        )));
    };

    let (response_tx, response_rx) = oneshot::channel();
    let job = Box::new(move |api: anvil::EthApi| -> EyreResult<()> {
        let response = process_rpc_request(api, forward);
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

    Ok(rpc_http_response(hooks::merge(
        response,
        screened.denied,
        screened.was_single,
    )))
}

#[get("/anvil/{id}")]
async fn forward_eth_json_rpc_ws_compat(
    state: web::Data<AppState>,
    params: web::Path<String>,
    req: HttpRequest,
    payload: web::Payload,
) -> Result<HttpResponse, actix_web::Error> {
    let id = params.into_inner();

    let (sender, chain_id, screener) = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => {
                let first_chain = node_entry.nodes.keys().min().copied();
                match first_chain {
                    Some(chain_id) => match node_entry.nodes.get(&chain_id) {
                        Some(node) => (node.sender.clone(), chain_id, node_entry.screener.clone()),
                        None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                    },
                    None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
                }
            }
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    eth_ws::start_json_rpc_websocket(&req, payload, id, sender, screener, chain_id)
}

#[get("/anvil/{id}/{chain_id}")]
async fn forward_eth_json_rpc_ws(
    state: web::Data<AppState>,
    params: web::Path<(String, u64)>,
    req: HttpRequest,
    payload: web::Payload,
) -> Result<HttpResponse, actix_web::Error> {
    let (id, chain_id) = params.into_inner();

    let (sender, screener) = {
        let nodes = state.nodes.lock().await;
        match nodes.get(&id) {
            Some(node_entry) => match node_entry.nodes.get(&chain_id) {
                Some(node) => (node.sender.clone(), node_entry.screener.clone()),
                None => return Ok(HttpResponse::NotFound().body("CHAIN_NOT_FOUND")),
            },
            None => return Ok(HttpResponse::NotFound().body("NOT_FOUND")),
        }
    };

    eth_ws::start_json_rpc_websocket(
        &req,
        payload,
        format!("{id}/{chain_id}"),
        sender,
        screener,
        chain_id,
    )
}

fn rpc_http_response(response: types::RpcForwardResult) -> HttpResponse {
    match response {
        Some(payload) => HttpResponse::Ok().json(payload),
        None => HttpResponse::NoContent().finish(),
    }
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
            .service(arm_node)
            .service(forward_eth_json_rpc)
            .service(forward_eth_json_rpc_compat)
            .service(forward_eth_json_rpc_ws)
            .service(forward_eth_json_rpc_ws_compat)
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_chain_ids_defaults_to_one() {
        assert_eq!(parse_chain_ids(None), vec![1]);
        assert_eq!(parse_chain_ids(Some("")), vec![1]);
        assert_eq!(parse_chain_ids(Some("abc")), vec![1]);
    }

    #[test]
    fn parse_chain_ids_reads_list() {
        assert_eq!(parse_chain_ids(Some("1,2,3")), vec![1, 2, 3]);
        assert_eq!(parse_chain_ids(Some(" 7 , 9 ")), vec![7, 9]);
    }

    #[test]
    fn resolve_chain_params_defaults_when_absent() {
        let params = resolve_chain_params(&[1], None, None);
        assert_eq!(params, vec![(anvil::DEFAULT_BLOCK_TIME, None)]);
    }

    #[test]
    fn resolve_chain_params_reads_parallel_lists() {
        let params = resolve_chain_params(&[1, 42], Some("5,2"), Some("fees,fifo"));
        assert_eq!(
            params,
            vec![(5, Some("fees".to_string())), (2, Some("fifo".to_string()))]
        );
    }

    #[test]
    fn resolve_chain_params_handles_short_lists() {
        // Only the first chain is described; the rest fall back to defaults.
        let params = resolve_chain_params(&[1, 2, 3], Some("5"), Some("fifo"));
        assert_eq!(params[0], (5, Some("fifo".to_string())));
        assert_eq!(params[1], (anvil::DEFAULT_BLOCK_TIME, None));
        assert_eq!(params[2], (anvil::DEFAULT_BLOCK_TIME, None));
    }

    #[test]
    fn resolve_chain_params_handles_unparseable_input() {
        let params = resolve_chain_params(&[1, 2], Some("abc,-1"), Some("bogus,"));
        assert_eq!(params[0], (anvil::DEFAULT_BLOCK_TIME, Some("bogus".to_string())));
        // The empty tx_order entry is dropped by split_csv, so it falls back.
        assert_eq!(params[1], (anvil::DEFAULT_BLOCK_TIME, None));
    }

    #[test]
    fn resolve_chain_params_preserves_explicit_zero() {
        let params = resolve_chain_params(&[1], Some("0"), None);
        assert_eq!(params, vec![(0, None)]);
    }
}
