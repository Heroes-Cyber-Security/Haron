mod anvil;
mod supervisor;

use actix_web::{App, HttpServer, Responder, post, web};
use eyre::{Result as EyreResult, eyre};
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
