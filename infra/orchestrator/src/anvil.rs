pub use anvil::eth::EthApi;
pub use anvil::NodeConfig;

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

	pub async fn shutdown(mut self) {
		if let Some(mut inner) = self.inner.take() {
			if let Some(signal) = inner.shutdown_signal_mut().take() {
				let _ = signal.fire();
			}

			for server in &inner.servers {
				server.abort();
			}
			while let Some(server) = inner.servers.pop() {
				let _ = server.await;
			}

			inner.node_service.abort();
			let _ = (&mut inner.node_service).await;
		}
	}
}

impl Deref for HeadlessNodeHandle {
	type Target = NodeHandle;

	fn deref(&self) -> &Self::Target {
		self.inner.as_ref().expect("HeadlessNodeHandle already consumed")
	}
}

impl DerefMut for HeadlessNodeHandle {
	fn deref_mut(&mut self) -> &mut Self::Target {
		self.inner.as_mut().expect("HeadlessNodeHandle already consumed")
	}
}

impl Drop for HeadlessNodeHandle {
	fn drop(&mut self) {
		if let Some(mut inner) = self.inner.take() {
			if let Some(signal) = inner.shutdown_signal_mut().take() {
				let _ = signal.fire();
			}

			for server in inner.servers.drain(..) {
				server.abort();
			}

			inner.node_service.abort();
		}
	}
}

/// Spawns an Anvil node without launching the HTTP/WS server tasks
pub async fn try_spawn(mut config: NodeConfig) -> Result<(EthApi, HeadlessNodeHandle)> {
	config.silent = true;
	config.enable_tracing = false;
	config.print_logs = false;
	config.print_traces = false;
	config.no_storage_caching = true;
	config.max_persisted_states = Some(0);
	config.host.clear();

	let (api, mut handle) = anvil::try_spawn(config).await?;

	if !handle.servers.is_empty() {
		handle.servers.iter().for_each(|server| server.abort());
		handle.servers.clear();
	}

	Ok((api, HeadlessNodeHandle::new(handle)))
}