pub use anvil::NodeConfig;
pub use anvil::cmd::NodeArgs;
pub use anvil::eth::EthApi;
pub use anvil::pubsub::{EthSubscription, LogsSubscription};

use clap_builder::Parser;
use anvil::NodeHandle;
use eyre::Result;
use std::ops::{Deref, DerefMut};

pub struct HeadlessNodeHandle {
    inner: Option<NodeHandle>,
}

impl HeadlessNodeHandle {
    fn new(inner: NodeHandle) -> Self {
        Self { inner: Some(inner) }
    }

    fn fire_shutdown_signal(inner: &mut NodeHandle) {
        if let Some(signal) = inner.shutdown_signal_mut().take() {
            if signal.fire().is_err() {
                eprintln!("failed to fire shutdown signal");
            }
        }
    }

    fn abort_servers(inner: &mut NodeHandle) {
        for server in inner.servers.drain(..) {
            server.abort();
        }
    }

    async fn abort_and_join_servers(inner: &mut NodeHandle) {
        for server in inner.servers.drain(..) {
            server.abort();
            match server.await {
                Ok(Ok(())) => {}
                Ok(Err(err)) => eprintln!("anvil server exited with error: {err:?}"),
                Err(err) if err.is_cancelled() => {}
                Err(err) => eprintln!("anvil server join error: {err:?}"),
            }
        }
    }

    pub async fn shutdown(mut self) {
        if let Some(mut inner) = self.inner.take() {
            Self::fire_shutdown_signal(&mut inner);
            Self::abort_and_join_servers(&mut inner).await;

            inner.node_service.abort();
            match (&mut inner.node_service).await {
                Ok(Ok(())) => {}
                Ok(Err(err)) => eprintln!("anvil node service exited with error: {err:?}"),
                Err(err) if err.is_cancelled() => {}
                Err(err) => eprintln!("anvil node service join error: {err:?}"),
            }
        }
    }
}

impl Deref for HeadlessNodeHandle {
    type Target = NodeHandle;

    fn deref(&self) -> &Self::Target {
        self.inner
            .as_ref()
            .expect("HeadlessNodeHandle already consumed")
    }
}

impl DerefMut for HeadlessNodeHandle {
    fn deref_mut(&mut self) -> &mut Self::Target {
        self.inner
            .as_mut()
            .expect("HeadlessNodeHandle already consumed")
    }
}

impl Drop for HeadlessNodeHandle {
    fn drop(&mut self) {
        if let Some(mut inner) = self.inner.take() {
            Self::fire_shutdown_signal(&mut inner);
            Self::abort_servers(&mut inner);

            inner.node_service.abort();
        }
    }
}

/// Spawns an Anvil node without launching the HTTP/WS server tasks
pub async fn try_spawn(config: NodeConfig) -> Result<(EthApi, HeadlessNodeHandle)> {
    let (api, mut handle) = anvil::try_spawn(config).await?;

    if !handle.servers.is_empty() {
        for server in handle.servers.drain(..) {
            server.abort();
            let _ = server.await;
        }
    }

    Ok((api, HeadlessNodeHandle::new(handle)))
}

pub fn create_config(chain_id: u64) -> NodeConfig {
    let chain_id_str = chain_id.to_string();
    let node_args: NodeArgs = NodeArgs::parse_from([
        "anvil",
        "--chain-id", &chain_id_str,
        "--accounts", "0",
        "--block-time", "1"
    ]);

    let mut config: NodeConfig = node_args.into_node_config().expect("Anvil NodeArgs transform to NodeConfig fail");

    config.silent = true;
    config.enable_tracing = false;
    config.print_logs = false;
    config.print_traces = false;
    config.no_storage_caching = true;
    config.max_persisted_states = Some(0);
    config.host.clear();

    config
}