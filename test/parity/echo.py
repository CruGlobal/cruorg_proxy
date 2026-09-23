import json, os, ssl
from http.server import ThreadingHTTPServer, BaseHTTPRequestHandler
NAME = os.environ["NAME"]
class H(BaseHTTPRequestHandler):
    def _go(self):
        n = int(self.headers.get("Content-Length") or 0)
        if n: self.rfile.read(n)
        body = json.dumps({"upstream": NAME, "sni": getattr(self.connection, "sni", None),
                           "method": self.command, "path": self.path,
                           "headers": {k.lower(): v for k, v in self.headers.items()}}).encode()
        self.send_response(200); self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body))); self.end_headers(); self.wfile.write(body)
    do_GET = do_POST = do_HEAD = do_PUT = do_DELETE = _go
    def log_message(self, *a): pass
ctx = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
ctx.load_cert_chain("/certs/cert.pem", "/certs/key.pem")
def sni(sock, name, c): sock.sni = name
ctx.sni_callback = sni
srv = ThreadingHTTPServer(("0.0.0.0", 443), H)
srv.socket = ctx.wrap_socket(srv.socket, server_side=True)
srv.serve_forever()
