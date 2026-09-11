pub use anvil::NodeConfig;
pub use anvil::cmd::NodeArgs;
pub use anvil::eth::EthApi;
pub use anvil::eth::pool::transactions::TransactionOrder;
pub use anvil::pubsub::{EthSubscription, LogsSubscription};

use anvil::NodeHandle;
use clap_builder::Parser;
use eyre::Result;
use std::ops::{Deref, DerefMut};

/// Interval mining period used when a chain does not request one.
pub const DEFAULT_BLOCK_TIME: u64 = 1;

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

/// Builds the node configuration for one chain.
///
/// `block_time` is the interval mining period in seconds. Passing 0 omits
/// `--block-time` entirely so the node mines on demand instead of on a timer.
/// `tx_order` selects the mempool ordering; an absent or unrecognised value
/// leaves the anvil default in place rather than failing the deploy.
pub fn create_config(chain_id: u64, block_time: u64, tx_order: Option<&str>) -> NodeConfig {
    let chain_id_str = chain_id.to_string();
    let block_time_str = block_time.to_string();

    let mut args: Vec<&str> = vec!["anvil", "--chain-id", chain_id_str.as_str(), "--accounts", "0"];

    if block_time > 0 {
        args.push("--block-time");
        args.push(block_time_str.as_str());
    }

    let node_args: NodeArgs = NodeArgs::parse_from(args);

    let mut config: NodeConfig = node_args
        .into_node_config()
        .expect("Anvil NodeArgs transform to NodeConfig fail");

    if let Some(order) = tx_order.and_then(|value| value.parse::<TransactionOrder>().ok()) {
        config.transaction_order = order;
    }

    config.silent = true;
    config.enable_tracing = false;
    config.print_logs = false;
    config.print_traces = false;
    config.no_storage_caching = true;
    config.max_persisted_states = Some(0);
    config.host.clear();

    config
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[test]
    fn create_config_honours_block_time() {
        let config = create_config(1, 5, None);
        assert_eq!(config.block_time, Some(Duration::from_secs(5)));
    }

    #[test]
    fn create_config_omits_block_time_when_zero() {
        let config = create_config(1, 0, None);
        assert_eq!(config.block_time, None);
    }

    #[test]
    fn create_config_applies_tx_order() {
        assert_eq!(
            create_config(1, 1, Some("fifo")).transaction_order,
            TransactionOrder::Fifo
        );
        assert_eq!(
            create_config(1, 1, Some("fees")).transaction_order,
            TransactionOrder::Fees
        );
    }

    #[test]
    fn create_config_keeps_default_on_unknown_tx_order() {
        assert_eq!(
            create_config(1, 1, Some("bogus")).transaction_order,
            TransactionOrder::default()
        );
        assert_eq!(
            create_config(1, 1, None).transaction_order,
            TransactionOrder::default()
        );
    }

    #[test]
    fn create_config_keeps_safety_settings() {
        let config = create_config(1, 5, Some("fees"));
        assert!(config.silent);
        assert!(!config.enable_tracing);
        assert!(config.no_storage_caching);
        assert_eq!(config.max_persisted_states, Some(0));
    }
}
