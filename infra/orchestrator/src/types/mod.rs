use anvil_rpc::response::Response as RpcResponseEnvelope;
use tokio::sync::mpsc;
use tokio::task::JoinHandle;

pub type RpcForwardResult = Option<RpcResponseEnvelope>;

pub struct NodeEntry {
    pub nodes: std::collections::HashMap<u64, SingleNodeEntry>,
}

pub struct SingleNodeEntry {
    pub api: crate::anvil::EthApi,
    pub sender: mpsc::Sender<crate::supervisor::Command>,
    pub task: JoinHandle<()>,
    pub chain_id: u64,
}
