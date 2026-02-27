use anvil_rpc::response::Response as RpcResponseEnvelope;
use tokio::sync::{mpsc, oneshot};
use tokio::task::JoinHandle;

pub type RpcForwardResult = Option<RpcResponseEnvelope>;

pub struct NodeEntry {
    pub nodes: std::collections::HashMap<u64, SingleNodeEntry>,
}

impl NodeEntry {
    pub async fn shutdown(self) {
        for (_, node) in self.nodes {
            node.shutdown().await;
        }
    }
}

pub struct SingleNodeEntry {
    pub api: crate::anvil::EthApi,
    pub sender: mpsc::Sender<crate::supervisor::Command>,
    pub task: JoinHandle<()>,
    pub chain_id: u64,
}

impl SingleNodeEntry {
    pub async fn shutdown(self) {
        let SingleNodeEntry { api, sender, task, .. } = self;
        drop(api);
        let (tx, rx) = oneshot::channel();
        let _ = sender
            .send(crate::supervisor::Command::Shutdown {
                respond_to: Some(tx),
            })
            .await;
        let _ = rx.await;
        let _ = task.await;
    }
}
