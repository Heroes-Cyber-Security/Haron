//! Challenge hook plumbing.
//!
//! Challenge authors implement hooks in `chal.py`, which lives inside the
//! worker. The orchestrator therefore only ever exchanges serialisable data
//! with the worker over HTTP:
//!
//! * [`fetch`] pulls the deploy-time pieces (genesis documents and precompile
//!   addresses) before any node is spawned.
//! * [`PythonPrecompileFactory`] turns a set of addresses into real EVM
//!   precompiles whose behaviour is a Python function.
//! * [`Screener`] consults `on_incoming_tx` before a transaction is forwarded,
//!   and blocks cheatcodes so the hook cannot be bypassed.
//!
//! The precompile callback runs on a supervisor blocking thread, so it uses the
//! blocking HTTP client. A hook must not call back into its own chain: the
//! supervisor serialises commands, so that would deadlock. Everything else
//! (hashing, signature checks, stateless oracles) is fair game, including from
//! internal `CALL`s.

use alloy_evm::precompiles::{DynPrecompile, PrecompileInput};
use alloy_evm::revm::precompile::{
    PrecompileId, PrecompileOutput, PrecompileResult, PrecompileStatus,
};
use alloy_genesis::Genesis;
use alloy_primitives::{Address, Bytes};
use anvil_rpc::{
    error::{ErrorCode, RpcError},
    request::{Request as RpcRequest, RpcCall},
    response::{Response as RpcResponseEnvelope, ResponseResult, RpcResponse},
};
use serde::Deserialize;
use serde_json::Value;
use std::collections::HashMap;
use std::str::FromStr;
use std::sync::{
    Arc,
    atomic::{AtomicBool, Ordering},
};
use std::time::Duration;

/// Budget for a single hook callback.
pub const DEFAULT_HOOK_TIMEOUT: Duration = Duration::from_secs(2);
/// Budget for the deploy-time metadata request, which may import a challenge
/// module and run `genesis()`/`precompile()` for the first time.
pub const METADATA_TIMEOUT: Duration = Duration::from_secs(30);

/// Deploy-time hook description returned by `GET /hooks/{hash}` on the worker.
#[derive(Debug, Default, Deserialize)]
struct HooksResponse {
    /// `{chainId: genesis.json contents}`.
    #[serde(default)]
    genesis: HashMap<String, Value>,
    /// `{chainId: [address, ...]}`; the callables stay in the worker.
    #[serde(default)]
    precompiles: HashMap<String, Vec<String>>,
    /// Whether `on_incoming_tx` is implemented rather than a stub.
    #[serde(default)]
    incoming_tx: bool,
}

/// Everything the orchestrator needs to know about a challenge's hooks.
#[derive(Debug, Clone)]
pub struct HookConfig {
    /// Base URL of the worker, e.g. `http://worker:8080`.
    pub worker_base: String,
    /// Challenge hash, used as the worker route segment.
    pub challenge: String,
    /// Instance id; the worker derives the player key from it.
    pub pea_id: String,
    /// Per-chain genesis documents.
    pub genesis: HashMap<u64, Genesis>,
    /// Per-chain precompile addresses backed by Python functions.
    pub precompiles: HashMap<u64, Vec<Address>>,
    /// Whether the transaction firewall is implemented.
    pub incoming_tx: bool,
}

impl HookConfig {
    /// Returns true when the challenge asked for no hook support at all.
    pub fn is_empty(&self) -> bool {
        self.genesis.is_empty() && self.precompiles.is_empty() && !self.incoming_tx
    }
}

/// Resolves the worker base URL, defaulting to the compose service name.
pub fn worker_base() -> String {
    std::env::var("WORKER_BASE").unwrap_or_else(|_| "http://worker:8080".to_string())
}

/// Fetches deploy-time hook metadata for `challenge`.
///
/// Returns `None` when the challenge has no hooks, when the worker is
/// unreachable, or when the response cannot be understood. Deploying without
/// hooks matches the behaviour of challenges that never implement any.
pub async fn fetch(
    worker_base: &str,
    challenge: &str,
    pea_id: &str,
    chain_ids: &[u64],
) -> Option<HookConfig> {
    let chains = chain_ids
        .iter()
        .map(u64::to_string)
        .collect::<Vec<_>>()
        .join(",");

    let client = reqwest::Client::builder()
        .timeout(METADATA_TIMEOUT)
        .build()
        .ok()?;

    let url = format!("{worker_base}/hooks/{challenge}");
    let response = match client
        .get(&url)
        .query(&[("pea_id", pea_id), ("chains", chains.as_str())])
        .send()
        .await
    {
        Ok(response) => response,
        Err(err) => {
            eprintln!("hooks: {challenge}: metadata request failed: {err}");
            return None;
        }
    };

    if !response.status().is_success() {
        eprintln!(
            "hooks: {challenge}: worker returned {} for metadata",
            response.status()
        );
        return None;
    }

    let parsed: HooksResponse = match response.json().await {
        Ok(parsed) => parsed,
        Err(err) => {
            eprintln!("hooks: {challenge}: invalid metadata payload: {err}");
            return None;
        }
    };

    let mut genesis = HashMap::new();
    for (chain, document) in parsed.genesis {
        let Ok(chain_id) = chain.parse::<u64>() else {
            eprintln!("hooks: {challenge}: ignoring genesis for invalid chain id {chain}");
            continue;
        };
        match serde_json::from_value::<Genesis>(document) {
            Ok(document) => {
                genesis.insert(chain_id, document);
            }
            Err(err) => {
                eprintln!("hooks: {challenge}: genesis for chain {chain} is invalid: {err}");
            }
        }
    }

    let mut precompiles = HashMap::new();
    for (chain, addresses) in parsed.precompiles {
        let Ok(chain_id) = chain.parse::<u64>() else {
            eprintln!("hooks: {challenge}: ignoring precompiles for invalid chain id {chain}");
            continue;
        };

        let parsed_addresses: Vec<Address> = addresses
            .iter()
            .filter_map(|address| match Address::from_str(address) {
                Ok(address) => Some(address),
                Err(err) => {
                    eprintln!("hooks: {challenge}: invalid precompile address {address}: {err}");
                    None
                }
            })
            .collect();

        if !parsed_addresses.is_empty() {
            precompiles.insert(chain_id, parsed_addresses);
        }
    }

    let config = HookConfig {
        worker_base: worker_base.to_string(),
        challenge: challenge.to_string(),
        pea_id: pea_id.to_string(),
        genesis,
        precompiles,
        incoming_tx: parsed.incoming_tx,
    };

    if config.is_empty() {
        None
    } else {
        Some(config)
    }
}

/// A precompile whose behaviour is a Python function in `chal.py`.
#[derive(Debug)]
pub struct PythonPrecompileFactory {
    backend: Arc<PrecompileBackend>,
    addresses: Vec<Address>,
}

struct PrecompileBackend {
    url: url::Url,
    pea_id: String,
    chain_id: u64,
}

impl std::fmt::Debug for PrecompileBackend {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("PrecompileBackend")
            .field("url", &self.url.as_str())
            .field("chain_id", &self.chain_id)
            .finish()
    }
}

/// Worker reply for one precompile invocation.
#[derive(Debug, Deserialize)]
struct PrecompileResponse {
    #[serde(default)]
    status: String,
    #[serde(default)]
    bytes: String,
}

impl PythonPrecompileFactory {
    /// Builds a factory for `addresses` on `chain_id`.
    pub fn new(hooks: &HookConfig, chain_id: u64, addresses: Vec<Address>) -> Option<Self> {
        let url = url::Url::parse(&format!(
            "{}/hooks/{}/precompile",
            hooks.worker_base, hooks.challenge
        ))
        .ok()?;

        Some(Self {
            backend: Arc::new(PrecompileBackend {
                url,
                pea_id: hooks.pea_id.clone(),
                chain_id,
            }),
            addresses,
        })
    }
}

impl anvil::PrecompileFactory for PythonPrecompileFactory {
    fn precompiles(&self) -> Vec<(Address, DynPrecompile)> {
        self.addresses
            .iter()
            .map(|address| {
                let address = *address;
                let backend = self.backend.clone();
                let id = PrecompileId::Custom(format!("har:{address}").into());
                let precompile =
                    DynPrecompile::new_stateful(id, move |input: PrecompileInput<'_>| {
                        backend.call(address, input)
                    });
                (address, precompile)
            })
            .collect()
    }
}

impl PrecompileBackend {
    fn call(&self, address: Address, input: PrecompileInput<'_>) -> PrecompileResult {
        let payload = serde_json::json!({
            "pea_id": self.pea_id,
            "chain_id": self.chain_id,
            "address": address.to_string(),
            "caller": input.caller.to_string(),
            "value": input.value.to_string(),
            "data": format!("0x{}", alloy_primitives::hex::encode(input.data)),
            "gas": input.gas,
            "is_static": input.is_static,
        });

        let response =
            post_json_blocking(&self.url, &payload, DEFAULT_HOOK_TIMEOUT).and_then(|body| {
                serde_json::from_str::<PrecompileResponse>(&body)
                    .map_err(|err| format!("invalid precompile response: {err}"))
            });

        match response {
            Ok(response) => Ok(PrecompileOutput {
                status: if response.status == "revert" {
                    PrecompileStatus::Revert
                } else {
                    PrecompileStatus::Success
                },
                gas_used: 0,
                gas_refunded: 0,
                state_gas_used: 0,
                state_gas_spilled: 0,
                reservoir: input.reservoir,
                bytes: Bytes::from(decode_hex(&response.bytes)),
            }),
            Err(err) => {
                eprintln!(
                    "hooks: precompile {address} on chain {} failed: {err}",
                    self.chain_id
                );
                Ok(revert_output(input.reservoir))
            }
        }
    }
}

/// Minimal blocking JSON POST over a plain TCP connection.
///
/// The precompile callback runs on a supervisor blocking thread, where an async
/// runtime is not available and a `reqwest::blocking` client would panic on
/// drop. Worker traffic is plain HTTP on the compose network, so a std-only
/// client is enough; TLS URLs are rejected rather than silently downgraded.
fn post_json_blocking(
    url: &url::Url,
    payload: &serde_json::Value,
    timeout: Duration,
) -> Result<String, String> {
    use std::io::{Read, Write};
    use std::net::TcpStream;

    if url.scheme() != "http" {
        return Err(format!("unsupported scheme {}", url.scheme()));
    }

    let host = url.host_str().ok_or("url has no host")?;
    let port = url.port_or_known_default().ok_or("url has no port")?;
    let path = match url.query() {
        Some(query) => format!("{}?{query}", url.path()),
        None => url.path().to_string(),
    };

    let body = serde_json::to_vec(payload).map_err(|err| err.to_string())?;

    let mut stream = TcpStream::connect((host, port)).map_err(|err| err.to_string())?;
    stream
        .set_read_timeout(Some(timeout))
        .map_err(|err| err.to_string())?;
    stream
        .set_write_timeout(Some(timeout))
        .map_err(|err| err.to_string())?;

    let request = format!(
        "POST {path} HTTP/1.1\r\nHost: {host}:{port}\r\nContent-Type: application/json\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
        body.len()
    );
    stream
        .write_all(request.as_bytes())
        .and_then(|()| stream.write_all(&body))
        .map_err(|err| err.to_string())?;

    let mut raw = Vec::new();
    stream
        .read_to_end(&mut raw)
        .map_err(|err| err.to_string())?;

    parse_http_response(&raw)
}

/// Splits an HTTP/1.1 response into status and body, decoding chunked bodies.
fn parse_http_response(raw: &[u8]) -> Result<String, String> {
    let split = raw
        .windows(4)
        .position(|window| window == b"\r\n\r\n")
        .ok_or("response has no header terminator")?;

    let head = String::from_utf8_lossy(&raw[..split]);
    let body = &raw[split + 4..];

    let status: u16 = head
        .lines()
        .next()
        .and_then(|line| line.split_whitespace().nth(1))
        .and_then(|code| code.parse().ok())
        .ok_or("response has no status line")?;

    if !(200..300).contains(&status) {
        return Err(format!("worker returned status {status}"));
    }

    let chunked = head.lines().any(|line| {
        line.to_ascii_lowercase()
            .starts_with("transfer-encoding: chunked")
    });

    let bytes = if chunked {
        decode_chunked(body)?
    } else {
        body.to_vec()
    };

    String::from_utf8(bytes).map_err(|err| err.to_string())
}

fn decode_chunked(mut body: &[u8]) -> Result<Vec<u8>, String> {
    let mut out = Vec::new();
    loop {
        let line_end = body
            .windows(2)
            .position(|window| window == b"\r\n")
            .ok_or("malformed chunk header")?;
        let header = String::from_utf8_lossy(&body[..line_end]);
        let size = usize::from_str_radix(header.split(';').next().unwrap_or("0").trim(), 16)
            .map_err(|err| err.to_string())?;
        body = &body[line_end + 2..];

        if size == 0 {
            return Ok(out);
        }
        if body.len() < size + 2 {
            return Err("truncated chunk".to_string());
        }

        out.extend_from_slice(&body[..size]);
        body = &body[size + 2..];
    }
}

fn revert_output(reservoir: u64) -> PrecompileOutput {
    PrecompileOutput {
        status: PrecompileStatus::Revert,
        gas_used: 0,
        gas_refunded: 0,
        state_gas_used: 0,
        state_gas_spilled: 0,
        reservoir,
        bytes: Bytes::new(),
    }
}

fn decode_hex(value: &str) -> Vec<u8> {
    let trimmed = value
        .strip_prefix("0x")
        .or_else(|| value.strip_prefix("0X"))
        .unwrap_or(value);
    alloy_primitives::hex::decode(trimmed).unwrap_or_default()
}

/// Consults `on_incoming_tx` before transactions are forwarded to a node.
#[derive(Clone, Debug)]
pub struct Screener {
    client: reqwest::Client,
    url: String,
    pea_id: String,
    armed: Arc<AtomicBool>,
}

impl Screener {
    /// Builds a screener. It stays inert until [`Screener::arm`] is called.
    pub fn new(hooks: &HookConfig, armed: Arc<AtomicBool>) -> Option<Self> {
        let client = reqwest::Client::builder()
            .timeout(DEFAULT_HOOK_TIMEOUT)
            .build()
            .ok()?;

        Some(Self {
            client,
            url: format!(
                "{}/hooks/{}/incoming_tx",
                hooks.worker_base, hooks.challenge
            ),
            pea_id: hooks.pea_id.clone(),
            armed,
        })
    }

    /// Starts screening. Called once the challenge's own setup transactions ran.
    pub fn arm(&self) {
        self.armed.store(true, Ordering::SeqCst);
    }

    /// Whether screening is currently active.
    pub fn is_armed(&self) -> bool {
        self.armed.load(Ordering::SeqCst)
    }
}

/// Worker reply for one firewall consultation.
#[derive(Debug, Deserialize)]
struct FirewallResponse {
    #[serde(default)]
    status: u16,
    #[serde(default)]
    message: String,
}

/// How a JSON-RPC method carries a transaction.
#[derive(Clone, Copy, PartialEq, Eq)]
enum SendShape {
    /// Serialised signed transaction in `params[0]`.
    Raw,
    /// Transaction request object in `params[0]`.
    Request,
}

fn send_shape(method: &str) -> Option<SendShape> {
    match method {
        "eth_sendRawTransaction"
        | "eth_sendRawTransactionSync"
        | "eth_sendRawTransactionConditional" => Some(SendShape::Raw),
        "eth_sendTransaction" | "eth_sendTransactionSync" | "eth_sendUnsignedTransaction" => {
            Some(SendShape::Request)
        }
        _ => None,
    }
}

fn is_cheatcode(method: &str) -> bool {
    method.starts_with("anvil_") || method.starts_with("hardhat_")
}

/// The decision for one method call.
enum Verdict {
    Allow,
    /// Drop the call and answer with `error`.
    Deny(RpcError),
}

impl Screener {
    async fn screen_call(&self, chain_id: u64, method: &str, params: &Value) -> Verdict {
        if !self.is_armed() {
            return Verdict::Allow;
        }

        // A firewall that can be bypassed with `anvil_impersonateAccount` and a
        // request-shaped transaction would only be advisory, so cheatcodes are
        // off while it is active.
        if is_cheatcode(method) {
            return Verdict::Deny(RpcError {
                code: ErrorCode::MethodNotFound,
                message: format!("{method} is disabled by the challenge firewall").into(),
                data: None,
            });
        }

        let Some(shape) = send_shape(method) else {
            return Verdict::Allow;
        };

        let first = params.get(0);
        let mut payload = serde_json::json!({
            "pea_id": self.pea_id,
            "chain_id": chain_id,
            "method": method,
        });

        match shape {
            SendShape::Raw => {
                let Some(raw) = first.and_then(Value::as_str) else {
                    return Verdict::Allow;
                };
                payload["raw"] = Value::String(raw.to_string());
            }
            SendShape::Request => {
                let Some(tx) = first.filter(|value| value.is_object()) else {
                    return Verdict::Allow;
                };
                payload["tx"] = tx.clone();
            }
        }

        let response = match self.client.post(&self.url).json(&payload).send().await {
            Ok(response) => response,
            Err(err) => {
                // Failing open keeps the challenge playable if the worker is
                // wedged; the node still enforces consensus rules.
                eprintln!("hooks: firewall request failed, allowing tx: {err}");
                return Verdict::Allow;
            }
        };

        let verdict: FirewallResponse = match response.json().await {
            Ok(verdict) => verdict,
            Err(err) => {
                eprintln!("hooks: firewall returned an invalid payload, allowing tx: {err}");
                return Verdict::Allow;
            }
        };

        if verdict.status < 400 {
            return Verdict::Allow;
        }

        Verdict::Deny(RpcError {
            code: ErrorCode::TransactionRejected,
            message: if verdict.message.is_empty() {
                format!("transaction rejected (status {})", verdict.status).into()
            } else {
                verdict.message.into()
            },
            data: None,
        })
    }
}

/// A request split into the part still to forward and the answers to synthesise.
pub struct Screened {
    /// Request to forward, `None` when every call was rejected.
    pub forward: Option<RpcRequest>,
    /// Responses for rejected calls.
    pub denied: Vec<RpcResponse>,
    /// Whether the original request was a single call (affects the envelope).
    pub was_single: bool,
}

/// Applies [`Screener`] to a whole JSON-RPC request.
pub async fn apply(screener: Option<&Screener>, chain_id: u64, request: RpcRequest) -> Screened {
    let was_single = matches!(request, RpcRequest::Single(_));

    let Some(screener) = screener else {
        return Screened {
            forward: Some(request),
            denied: Vec::new(),
            was_single,
        };
    };

    if !screener.is_armed() {
        return Screened {
            forward: Some(request),
            denied: Vec::new(),
            was_single,
        };
    }

    match request {
        RpcRequest::Single(call) => match screen_one(screener, chain_id, call).await {
            ScreenedCall::Forward(call) => Screened {
                forward: Some(RpcRequest::Single(call)),
                denied: Vec::new(),
                was_single,
            },
            ScreenedCall::Deny(response) => Screened {
                forward: None,
                denied: vec![response],
                was_single,
            },
            ScreenedCall::Drop => Screened {
                forward: None,
                denied: Vec::new(),
                was_single,
            },
        },
        RpcRequest::Batch(calls) => {
            let mut allowed = Vec::with_capacity(calls.len());
            let mut denied = Vec::new();

            for call in calls {
                match screen_one(screener, chain_id, call).await {
                    ScreenedCall::Forward(call) => allowed.push(call),
                    ScreenedCall::Deny(response) => denied.push(response),
                    ScreenedCall::Drop => {}
                }
            }

            let forward = if allowed.is_empty() {
                None
            } else {
                Some(RpcRequest::Batch(allowed))
            };

            Screened {
                forward,
                denied,
                was_single,
            }
        }
    }
}

enum ScreenedCall {
    Forward(RpcCall),
    Deny(RpcResponse),
    Drop,
}

async fn screen_one(screener: &Screener, chain_id: u64, call: RpcCall) -> ScreenedCall {
    match call {
        RpcCall::MethodCall(method_call) => {
            let params: Value = method_call.params.clone().into();
            match screener
                .screen_call(chain_id, &method_call.method, &params)
                .await
            {
                Verdict::Allow => ScreenedCall::Forward(RpcCall::MethodCall(method_call)),
                Verdict::Deny(error) => ScreenedCall::Deny(RpcResponse::new(
                    method_call.id,
                    ResponseResult::Error(error),
                )),
            }
        }
        RpcCall::Notification(notification) => {
            let params: Value = notification.params.clone().into();
            match screener
                .screen_call(chain_id, &notification.method, &params)
                .await
            {
                Verdict::Allow => ScreenedCall::Forward(RpcCall::Notification(notification)),
                // A notification has no id to answer, so a rejected one is
                // simply dropped.
                Verdict::Deny(_) => ScreenedCall::Drop,
            }
        }
        RpcCall::Invalid { .. } => ScreenedCall::Forward(call),
    }
}

/// Recombines forwarded responses with synthesised rejections.
pub fn merge(
    forwarded: Option<RpcResponseEnvelope>,
    denied: Vec<RpcResponse>,
    was_single: bool,
) -> Option<RpcResponseEnvelope> {
    if denied.is_empty() {
        return forwarded;
    }

    // A single request is answered with a single response object, never a batch.
    if was_single {
        return denied.into_iter().next().map(RpcResponseEnvelope::Single);
    }

    let mut responses = match forwarded {
        Some(RpcResponseEnvelope::Single(response)) => vec![response],
        Some(RpcResponseEnvelope::Batch(responses)) => responses,
        None => Vec::new(),
    };
    responses.extend(denied);

    if responses.is_empty() {
        None
    } else {
        Some(RpcResponseEnvelope::Batch(responses))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn send_shape_covers_every_send_path() {
        for method in [
            "eth_sendRawTransaction",
            "eth_sendRawTransactionSync",
            "eth_sendRawTransactionConditional",
        ] {
            assert!(matches!(send_shape(method), Some(SendShape::Raw)));
        }

        for method in [
            "eth_sendTransaction",
            "eth_sendTransactionSync",
            "eth_sendUnsignedTransaction",
        ] {
            assert!(matches!(send_shape(method), Some(SendShape::Request)));
        }

        for method in ["eth_call", "eth_getBalance", "eth_chainId"] {
            assert!(send_shape(method).is_none());
        }
    }

    #[test]
    fn cheatcodes_are_recognised() {
        assert!(is_cheatcode("anvil_impersonateAccount"));
        assert!(is_cheatcode("anvil_setBalance"));
        assert!(is_cheatcode("hardhat_setBalance"));
        assert!(!is_cheatcode("eth_sendTransaction"));
        assert!(!is_cheatcode("evm_mine"));
    }

    #[test]
    fn merge_keeps_single_envelope_for_single_requests() {
        let denied = vec![RpcResponse::invalid_request(
            anvil_rpc::request::Id::Number(7),
        )];
        let merged = merge(None, denied, true);
        assert!(matches!(merged, Some(RpcResponseEnvelope::Single(_))));
    }

    #[test]
    fn merge_produces_batch_for_batch_requests() {
        let denied = vec![RpcResponse::invalid_request(
            anvil_rpc::request::Id::Number(7),
        )];
        let merged = merge(None, denied, false);
        assert!(matches!(merged, Some(RpcResponseEnvelope::Batch(_))));
    }

    #[test]
    fn merge_passes_through_when_nothing_was_denied() {
        assert!(merge(None, Vec::new(), true).is_none());
    }

    #[test]
    fn decode_hex_tolerates_prefixes() {
        assert_eq!(decode_hex("0xdead"), vec![0xde, 0xad]);
        assert_eq!(decode_hex("dead"), vec![0xde, 0xad]);
        assert_eq!(decode_hex("0x"), Vec::<u8>::new());
        assert_eq!(decode_hex("not hex"), Vec::<u8>::new());
    }

    #[test]
    fn empty_hook_config_is_reported_as_empty() {
        let config = HookConfig {
            worker_base: "http://worker:8080".into(),
            challenge: "demo".into(),
            pea_id: "pea".into(),
            genesis: HashMap::new(),
            precompiles: HashMap::new(),
            incoming_tx: false,
        };
        assert!(config.is_empty());
    }

    #[test]
    fn factory_exposes_one_precompile_per_address() {
        let config = HookConfig {
            worker_base: "http://worker:8080".into(),
            challenge: "demo".into(),
            pea_id: "pea".into(),
            genesis: HashMap::new(),
            precompiles: HashMap::new(),
            incoming_tx: false,
        };

        let addresses = vec![Address::with_last_byte(0x42), Address::with_last_byte(0x43)];
        let factory = PythonPrecompileFactory::new(&config, 1, addresses.clone())
            .expect("factory should build");

        let installed: Vec<Address> = anvil::PrecompileFactory::precompiles(&factory)
            .into_iter()
            .map(|(address, _)| address)
            .collect();
        assert_eq!(installed, addresses);
    }

    #[test]
    fn screener_starts_disarmed_and_arms_on_request() {
        let config = HookConfig {
            worker_base: "http://worker:8080".into(),
            challenge: "demo".into(),
            pea_id: "pea".into(),
            genesis: HashMap::new(),
            precompiles: HashMap::new(),
            incoming_tx: true,
        };

        let screener = Screener::new(&config, Default::default()).expect("screener should build");
        assert!(
            !screener.is_armed(),
            "setup transactions must not be screened"
        );
        screener.arm();
        assert!(screener.is_armed());
    }

    #[test]
    fn parse_http_response_reads_plain_body() {
        let raw = b"HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\n{}";
        assert_eq!(parse_http_response(raw).unwrap(), "{}");
    }

    #[test]
    fn parse_http_response_decodes_chunked_body() {
        let raw =
            b"HTTP/1.1 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n7\r\n{\"a\":1}\r\n0\r\n\r\n";
        assert_eq!(parse_http_response(raw).unwrap(), "{\"a\":1}");
    }

    #[test]
    fn parse_http_response_rejects_error_status() {
        let raw = b"HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n";
        assert!(parse_http_response(raw).is_err());
    }

    #[test]
    fn post_json_blocking_rejects_tls() {
        let url = url::Url::parse("https://worker:8080/hooks/demo/precompile").unwrap();
        assert!(post_json_blocking(&url, &serde_json::json!({}), DEFAULT_HOOK_TIMEOUT).is_err());
    }
}
