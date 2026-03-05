from http.server import BaseHTTPRequestHandler, HTTPServer
import json
import socket

#!/usr/bin/env python3
# Simple HTTP server with one endpoint: /api/v1/user/me
# Save as /home/hanz/OpenSource/MockCTFD/app.py


HOST = "0.0.0.0"
PORT = 9090


class SimpleHandler(BaseHTTPRequestHandler):
    def _send_json(self, obj, status=200):
        body = json.dumps(obj).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json; charset=utf-8")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        path = self.path.split("?", 1)[0].rstrip("/")
        if path == "/api/v1/users/me":
            resp = {"success": True, "data": {"id": 1, "name": "Player"}}
            self._send_json(resp, status=200)
        else:
            self._send_json({"success": False, "error": "not found"}, status=404)

    def log_message(self, format, *args):
        # suppress default logging or customize as needed
        return


def run():
    server = HTTPServer((HOST, PORT), SimpleHandler)
    # try to set SO_REUSEADDR to reduce "address already in use" issues on restart
    try:
        server.socket.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
    except Exception:
        pass
    print(f"Listening on http://{HOST}:{PORT}")
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    run()
