use crate::anvil::{self, EthApi, HeadlessNodeHandle, NodeConfig};
use eyre::{Result as EyreResult, eyre};
use std::panic::{AssertUnwindSafe, catch_unwind};
use tokio::sync::{mpsc, oneshot};
use tokio::task::JoinHandle;

/// A boxed job executed against an `EthApi` instance under panic isolation.
pub type Job = Box<dyn FnOnce(EthApi) -> EyreResult<()> + Send + 'static>;

/// Optional response channel for an executed job.
pub type JobResponse = oneshot::Sender<EyreResult<()>>;

/// Commands accepted by the supervisor.
pub enum Command {
    /// Execute the provided job against the managed Anvil instance.
    Execute {
        job: Job,
        respond_to: Option<JobResponse>,
    },
    /// Gracefully shut down the managed Anvil instance.
    Shutdown {
        respond_to: Option<oneshot::Sender<()>>,
    },
}

/// Spawns the supervisor loop that manages a single Anvil instance.
pub fn spawn(mut rx: mpsc::Receiver<Command>, config: NodeConfig) -> JoinHandle<()> {
    tokio::spawn(async move {
        let mut node = spawn_node(&config).await.ok();

        while let Some(command) = rx.recv().await {
            match command {
                Command::Execute { job, respond_to } => {
                    if node.is_none() {
                        match spawn_node(&config).await {
                            Ok(new_node) => node = Some(new_node),
                            Err(err) => {
                                eprintln!("failed to spawn Anvil instance: {err:?}");
                                send_job_result(respond_to, Err(err));
                                continue;
                            }
                        }
                    }

                    let Some(active_node) = node.as_ref() else {
                        send_job_result(
                            respond_to,
                            Err(eyre!("Anvil instance unavailable after respawn failure")),
                        );
                        continue;
                    };

                    let api = active_node.api.clone();
                    let job_result = tokio::task::spawn_blocking(move || {
                        catch_unwind(AssertUnwindSafe(|| job(api)))
                    })
                    .await;

                    let mut should_shutdown = false;

                    match job_result {
                        Ok(Ok(Ok(()))) => {
                            send_job_result(respond_to, Ok(()));
                        }
                        Ok(Ok(Err(err))) => {
                            send_job_result(respond_to, Err(err));
                        }
                        Ok(Err(_panic)) => {
                            eprintln!("execution against Anvil panicked; shutting down node");
                            send_job_result(respond_to, Err(eyre!("execution panicked")));
                            should_shutdown = true;
                        }
                        Err(join_err) => {
                            eprintln!("blocking execute task failed: {join_err:?}");
                            send_job_result(
                                respond_to,
                                Err(eyre!("blocking execute task failed: {join_err}")),
                            );
                            should_shutdown = true;
                        }
                    }

                    if should_shutdown {
                        if let Some(old_node) = node.take() {
                            old_node.shutdown().await;
                        }

                        break;
                    }
                }
                Command::Shutdown { respond_to } => {
                    if let Some(old_node) = node.take() {
                        old_node.shutdown().await;
                    }

                    if let Some(tx) = respond_to {
                        let _ = tx.send(());
                    }

                    break;
                }
            }
        }

        if let Some(old_node) = node {
            old_node.shutdown().await;
        }
    })
}

struct RunningNode {
    api: EthApi,
    handle: HeadlessNodeHandle,
}

impl RunningNode {
    async fn shutdown(self) {
        self.handle.shutdown().await;
    }
}

async fn spawn_node(config: &NodeConfig) -> EyreResult<RunningNode> {
    let (api, handle) = anvil::try_spawn(config.clone()).await?;
    Ok(RunningNode { api, handle })
}

fn send_job_result(target: Option<JobResponse>, result: EyreResult<()>) {
    if let Some(tx) = target {
        let _ = tx.send(result);
    }
}
