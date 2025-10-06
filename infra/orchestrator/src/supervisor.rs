use crate::anvil::{
    self, EthApi, EthSubscription, HeadlessNodeHandle, LogsSubscription, NodeConfig,
};
use alloy_rpc_types::{
    FilteredParams,
    pubsub::{Params as SubscriptionParams, SubscriptionKind},
};
use anvil_core::eth::subscription::SubscriptionId;
use anvil_rpc::error::RpcError;
use eyre::{Result as EyreResult, eyre};
use std::{
    collections::VecDeque,
    panic::{AssertUnwindSafe, catch_unwind},
};
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
    /// Create a pubsub subscription against the managed Anvil instance.
    Subscribe {
        request: SubscriptionRequest,
        respond_to: oneshot::Sender<Result<SubscriptionSetup, RpcError>>,
    },
    /// Gracefully shut down the managed Anvil instance.
    Shutdown {
        respond_to: Option<oneshot::Sender<()>>,
    },
}

/// Request payload describing the subscription to create.
pub struct SubscriptionRequest {
    pub kind: SubscriptionKind,
    pub params: Box<SubscriptionParams>,
}

/// Successful subscription setup result containing the new ID and stream.
pub struct SubscriptionSetup {
    pub id: SubscriptionId,
    pub stream: EthSubscription,
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
                Command::Subscribe {
                    request,
                    respond_to,
                } => {
                    if node.is_none() {
                        match spawn_node(&config).await {
                            Ok(new_node) => node = Some(new_node),
                            Err(err) => {
                                eprintln!(
                                    "failed to spawn Anvil instance for subscription: {err:?}"
                                );
                                let _ = respond_to.send(Err(RpcError::internal_error_with(
                                    "Anvil instance unavailable",
                                )));
                                continue;
                            }
                        }
                    }

                    let Some(active_node) = node.as_ref() else {
                        let _ = respond_to.send(Err(RpcError::internal_error_with(
                            "Anvil instance unavailable",
                        )));
                        continue;
                    };

                    let result = create_subscription(&active_node.api, request).map_err(|err| {
                        eprintln!("failed to create subscription: {err:?}");
                        err
                    });

                    if respond_to.send(result).is_err() {
                        eprintln!("subscription requester dropped response channel");
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

fn create_subscription(
    api: &EthApi,
    request: SubscriptionRequest,
) -> Result<SubscriptionSetup, RpcError> {
    let SubscriptionRequest { kind, params } = request;
    let raw_params = *params;
    let id = SubscriptionId::random_hex();

    let subscription = match kind {
        SubscriptionKind::Logs => {
            if raw_params.is_bool() {
                return Err(RpcError::invalid_params(
                    "Expected params for logs subscription",
                ));
            }

            let filter = match raw_params {
                SubscriptionParams::None => None,
                SubscriptionParams::Logs(filter) => Some(filter),
                SubscriptionParams::Bool(_) => None,
            };

            let params = FilteredParams::new(filter.map(|b| *b));
            EthSubscription::Logs(Box::new(LogsSubscription {
                blocks: api.new_block_notifications(),
                storage: api.storage_info(),
                filter: params,
                queued: VecDeque::default(),
                id: id.clone(),
            }))
        }
        SubscriptionKind::NewHeads => EthSubscription::Header(
            api.new_block_notifications(),
            api.storage_info(),
            id.clone(),
        ),
        SubscriptionKind::NewPendingTransactions => match raw_params {
            SubscriptionParams::Bool(true) => EthSubscription::FullPendingTransactions(
                api.full_pending_transactions(),
                id.clone(),
            ),
            SubscriptionParams::Bool(false) | SubscriptionParams::None => {
                EthSubscription::PendingTransactions(api.new_ready_transactions(), id.clone())
            }
            SubscriptionParams::Logs(_) => {
                return Err(RpcError::invalid_params(
                    "Expected boolean parameter for newPendingTransactions",
                ));
            }
        },
        SubscriptionKind::Syncing => {
            return Err(RpcError::internal_error_with("Not implemented"));
        }
    };

    Ok(SubscriptionSetup {
        id,
        stream: subscription,
    })
}
