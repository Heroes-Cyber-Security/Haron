use super::session::JsonRpcWebsocketSession;

use crate::supervisor;

use actix_web::{HttpRequest, HttpResponse, web};
use actix_web_actors::ws;
use tokio::sync::mpsc;

pub fn start_json_rpc_websocket(
    req: &HttpRequest,
    payload: web::Payload,
    node_id: String,
    sender: mpsc::Sender<supervisor::Command>,
) -> Result<HttpResponse, actix_web::Error> {
    let session = JsonRpcWebsocketSession::new(node_id, sender);
    ws::start(session, req, payload)
}
