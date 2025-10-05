mod anvil;

use actix_web::{post, web, App, HttpServer, Responder};
use std::collections::HashMap;
use tokio::sync::Mutex;

struct AppState {
    nodes: Mutex<HashMap<String, NodeEntry>>,
}

impl AppState {
    fn new() -> Self {
        Self { nodes: Mutex::new(HashMap::new()) }
    }
}

struct NodeEntry {
    api: anvil::EthApi,
    handle: anvil::HeadlessNodeHandle,
}

impl NodeEntry {
    fn new(api: anvil::EthApi, handle: anvil::HeadlessNodeHandle) -> Self {
        Self { api, handle }
    }

    async fn shutdown(self) {
        let NodeEntry { api, handle } = self;
        drop(api);
        handle.shutdown().await;
    }
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
    let spawn_res = anvil::try_spawn(config).await;
    let (api, handle) = match spawn_res {
        Ok(pair) => pair,
        Err(e) => {
            eprintln!("failed to spawn anvil: {:?}", e);
            return "ERR";
        }
    };

    let mut nodes = state.nodes.lock().await;
    nodes.insert(id, NodeEntry::new(api, handle));
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
    })
    .bind("0.0.0.0:8080")?
    .run()
    .await
}
