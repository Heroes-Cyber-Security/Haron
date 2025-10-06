use super::helper::{build_eth_request, map_serde_error};

use anvil_rpc::{
    request::{Request as RpcRequest, RpcCall, RpcMethodCall, RpcNotification},
    response::{Response as RpcResponseEnvelope, ResponseResult, RpcResponse},
};

pub fn execute_rpc_request(
    handle: &tokio::runtime::Handle,
    api: &crate::anvil::EthApi,
    request: RpcRequest,
) -> crate::types::RpcForwardResult {
    match request {
        RpcRequest::Single(call) => {
            handle_rpc_call(handle, api, call).map(RpcResponseEnvelope::Single)
        }
        RpcRequest::Batch(calls) => {
            let mut responses = Vec::with_capacity(calls.len());
            for call in calls {
                if let Some(response) = handle_rpc_call(handle, api, call) {
                    responses.push(response);
                }
            }
            if responses.is_empty() {
                None
            } else {
                Some(RpcResponseEnvelope::Batch(responses))
            }
        }
    }
}

pub fn handle_rpc_call(
    handle: &tokio::runtime::Handle,
    api: &crate::anvil::EthApi,
    call: RpcCall,
) -> Option<RpcResponse> {
    match call {
        RpcCall::MethodCall(method_call) => Some(handle_method_call(handle, api, method_call)),
        RpcCall::Notification(notification) => {
            handle_notification(handle, api, notification);
            None
        }
        RpcCall::Invalid { id } => Some(RpcResponse::invalid_request(id)),
    }
}

pub fn handle_method_call(
    handle: &tokio::runtime::Handle,
    api: &crate::anvil::EthApi,
    call: RpcMethodCall,
) -> RpcResponse {
    let RpcMethodCall {
        method, params, id, ..
    } = call;
    match build_eth_request(method, params) {
        Ok(eth_request) => {
            let result = handle.block_on(api.execute(eth_request));
            RpcResponse::new(id, result)
        }
        Err(err) => {
            let rpc_error = map_serde_error(err);
            RpcResponse::new(id, ResponseResult::Error(rpc_error))
        }
    }
}

pub fn handle_notification(
    handle: &tokio::runtime::Handle,
    api: &crate::anvil::EthApi,
    notification: RpcNotification,
) {
    let RpcNotification { method, params, .. } = notification;
    match build_eth_request(method, params) {
        Ok(eth_request) => {
            let _ = handle.block_on(api.execute(eth_request));
        }
        Err(err) => {
            eprintln!("failed to parse json-rpc notification: {err}");
        }
    }
}
