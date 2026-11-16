pub use anvil::NodeConfig;
pub use anvil::cmd::NodeArgs;
pub use anvil::eth::EthApi as AnvilEthApi;
pub use anvil::eth::pool::transactions::TransactionOrder;
pub use anvil::pubsub::EthSubscription as AnvilEthSubscription;
pub use anvil::pubsub::LogsSubscription as AnvilLogsSubscription;

use anvil::NodeHandle;
use clap_builder::Parser;
use eyre::Result;
use foundry_primitives::FoundryNetwork;
use std::ops::{Deref, DerefMut};

/// The concrete Anvil node API (generic over the foundry network).
pub type EthApi = AnvilEthApi<FoundryNetwork>;
/// A live eth pubsub stream, materialized for the foundry network.
pub type EthSubscription = AnvilEthSubscription<FoundryNetwork>;
/// Log subscription stream used by `EthSubscription::Logs`.
pub type LogsSubscription = AnvilLogsSubscription<FoundryNetwork>;

/// Interval mining period used when a chain does not request one.
pub const DEFAULT_BLOCK_TIME: u64 = 1;

pub struct HeadlessNodeHandle {
    inner: Option<NodeHandle>,
}

impl HeadlessNodeHandle {
    fn new(inner: NodeHandle) -> Self {
        Self { inner: Some(inner) }
    }

    /// Terminates the managed Anvil instance.
    ///
    /// Foundry v1.9 keeps `NodeHandle`'s `servers`/`node_service` fields private;
    /// its `Drop` implementation fires the shutdown signal and aborts the node
    /// service, server, and ipc tasks, so we can rely on dropping the handle.
    pub async fn shutdown(mut self) {
        drop(self.inner.take());
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
        drop(self.inner.take());
    }
}

/// Spawns an Anvil node without launching the HTTP/WS server tasks
pub async fn try_spawn(config: NodeConfig) -> Result<(EthApi, HeadlessNodeHandle)> {
    let (api, handle) = anvil::try_spawn(config).await?;

    Ok((api, HeadlessNodeHandle::new(handle)))
}

/// Builds the node configuration for one chain.
///
/// `block_time` is the interval mining period in seconds. Passing 0 omits
/// `--block-time` entirely so the node mines on demand instead of on a timer.
/// `tx_order` selects the mempool ordering; an absent or unrecognised value
/// leaves the anvil default in place rather than failing the deploy.
///
/// `hooks` attaches the challenge's genesis document and Python-backed
/// precompiles for this chain, when the challenge declares any.
pub fn create_config(
    chain_id: u64,
    block_time: u64,
    tx_order: Option<&str>,
    hooks: Option<&crate::hooks::HookConfig>,
) -> NodeConfig {
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

    if let Some(hooks) = hooks {
        if let Some(genesis) = hooks.genesis.get(&chain_id) {
            // Anvil derives the active hardfork from the genesis timestamp, so a
            // document without one (the common case) would otherwise pin the
            // chain to Frontier and disable STATICCALL, PUSH0 and friends.
            if genesis.timestamp == 0 {
                config.genesis_timestamp = Some(
                    std::time::SystemTime::now()
                        .duration_since(std::time::UNIX_EPOCH)
                        .map(|elapsed| elapsed.as_secs())
                        .unwrap_or(0),
                );
            }
            config.genesis = Some(genesis.clone());
        }

        if let Some(addresses) = hooks.precompiles.get(&chain_id) {
            if let Some(factory) =
                crate::hooks::PythonPrecompileFactory::new(hooks, chain_id, addresses.clone())
            {
                config.precompile_factory = Some(std::sync::Arc::new(factory));
            }
        }
    }

    config
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::time::Duration;

    #[test]
    fn create_config_honours_block_time() {
        let config = create_config(1, 5, None, None);
        assert_eq!(config.block_time, Some(Duration::from_secs(5)));
    }

    #[test]
    fn create_config_omits_block_time_when_zero() {
        let config = create_config(1, 0, None, None);
        assert_eq!(config.block_time, None);
    }

    #[test]
    fn create_config_applies_tx_order() {
        assert_eq!(
            create_config(1, 1, Some("fifo"), None).transaction_order,
            TransactionOrder::Fifo
        );
        assert_eq!(
            create_config(1, 1, Some("fees"), None).transaction_order,
            TransactionOrder::Fees
        );
    }

    #[test]
    fn create_config_keeps_default_on_unknown_tx_order() {
        assert_eq!(
            create_config(1, 1, Some("bogus"), None).transaction_order,
            TransactionOrder::default()
        );
        assert_eq!(
            create_config(1, 1, None, None).transaction_order,
            TransactionOrder::default()
        );
    }

    #[test]
    fn create_config_keeps_safety_settings() {
        let config = create_config(1, 5, Some("fees"), None);
        assert!(config.silent);
        assert!(!config.enable_tracing);
        assert!(config.no_storage_caching);
        assert_eq!(config.max_persisted_states, Some(0));
    }

    #[test]
    fn create_config_attaches_genesis_for_the_chain() {
        let mut genesis = std::collections::HashMap::new();
        genesis.insert(1, alloy_genesis::Genesis::default());

        let hooks = crate::hooks::HookConfig {
            worker_base: "http://worker:8080".into(),
            challenge: "demo".into(),
            pea_id: "pea".into(),
            genesis,
            precompiles: std::collections::HashMap::new(),
            incoming_tx: false,
        };

        let config = create_config(1, 1, None, Some(&hooks));
        assert!(config.genesis.is_some());
        // A genesis without a timestamp would otherwise pin the chain to the
        // earliest hardfork.
        assert!(config.genesis_timestamp.unwrap_or(0) > 0);
        // A chain without a document keeps anvil's own genesis.
        assert!(create_config(2, 1, None, Some(&hooks)).genesis.is_none());
    }

    #[test]
    fn create_config_attaches_precompiles_for_the_chain() {
        let mut precompiles = std::collections::HashMap::new();
        precompiles.insert(1, vec![alloy_primitives::Address::with_last_byte(0x42)]);

        let hooks = crate::hooks::HookConfig {
            worker_base: "http://worker:8080".into(),
            challenge: "demo".into(),
            pea_id: "pea".into(),
            genesis: std::collections::HashMap::new(),
            precompiles,
            incoming_tx: false,
        };

        assert!(
            create_config(1, 1, None, Some(&hooks))
                .precompile_factory
                .is_some()
        );
        assert!(
            create_config(2, 1, None, Some(&hooks))
                .precompile_factory
                .is_none()
        );
    }
}
