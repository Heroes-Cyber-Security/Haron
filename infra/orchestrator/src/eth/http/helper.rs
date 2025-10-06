use anvil_core::eth::EthRequest;
use anvil_rpc::{error::RpcError, request::RequestParams};
use serde_json::{Value, json};

pub fn build_eth_request(
    method: String,
    params: RequestParams,
) -> Result<EthRequest, serde_json::Error> {
    let params_value: Value = params.into();
    let value = json!({ "method": method, "params": params_value });
    serde_json::from_value(value)
}

pub fn map_serde_error(err: serde_json::Error) -> RpcError {
    use serde_json::error::Category;
    match err.classify() {
        Category::Data if err.to_string().contains("unknown variant") => {
            RpcError::method_not_found()
        }
        Category::Data => RpcError::invalid_params(err.to_string()),
        Category::Syntax => RpcError::parse_error(),
        Category::Io | Category::Eof => RpcError::internal_error(),
    }
}
