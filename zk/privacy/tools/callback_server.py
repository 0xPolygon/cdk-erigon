#!/usr/bin/env python3
import argparse
from http.server import BaseHTTPRequestHandler, HTTPServer
import json


class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        content_length = int(self.headers.get('Content-Length', 0))
        body = self.rfile.read(content_length)
        try:
            data = json.loads(body.decode('utf-8'))
        except Exception:
            data = {"raw": body.decode('utf-8', errors='ignore')}
        with open(self.server.output_path, 'w') as f:
            json.dump(data, f)
        self.send_response(200)
        self.send_header('Content-Type', 'application/json')
        self.end_headers()
        self.wfile.write(b'{"status":"ok"}')


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument('--host', default='0.0.0.0')
    ap.add_argument('--port', type=int, default=8787)
    ap.add_argument('--output', default='proof.json')
    args = ap.parse_args()

    httpd = HTTPServer((args.host, args.port), Handler)
    httpd.output_path = args.output
    print(f"Callback server listening on http://{args.host}:{args.port}\nWill write proof to {args.output}")
    httpd.serve_forever()


if __name__ == '__main__':
    main()

