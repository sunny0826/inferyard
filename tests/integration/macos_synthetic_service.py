"""An owned HTTP fixture, with no weights or inference engine; not a model benchmark."""

import json
import sys
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from inferyard.adapters.prism import BUILD


def main():
    model = sys.argv[sys.argv.index("--model") + 1]
    params = {}

    class Handler(BaseHTTPRequestHandler):
        def log_message(self, *args):
            pass

        def respond(self, data, content_type="application/json"):
            raw = data if isinstance(data, bytes) else json.dumps(data).encode()
            self.send_response(200)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self):
            if self.path == "/props":
                self.respond(
                    {
                        "build_info": BUILD,
                        "total_slots": 1,
                        "default_generation_settings": {"n_ctx": 4096},
                        "model_path": model,
                        "chat_template": "fixture-template",
                    }
                )
            elif self.path == "/slots":
                self.respond([{"id": 0, "is_processing": False, "params": params, "n_ctx": 4096}])
            else:
                self.send_error(404)

        def do_POST(self):
            body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
            if self.path == "/apply-template":
                self.respond({"prompt": "fixture"})
            elif self.path == "/tokenize":
                self.respond({"tokens": [1]})
            elif self.path == "/v1/chat/completions":
                params.update(body)
                time.sleep(0.08)
                answer = "北京"
                if "姓名" in body["messages"][0]["content"]:
                    answer = '{"name":"小明","age":12}'
                event = {
                    "choices": [{"index": 0, "finish_reason": "stop"}],
                    "usage": {"prompt_tokens": 1, "completion_tokens": 2},
                }
                if body["stream"]:
                    event["choices"][0]["delta"] = {"content": answer}
                    raw = ("data: " + json.dumps(event) + "\n\ndata: [DONE]\n\n").encode()
                    self.respond(raw, "text/event-stream")
                else:
                    event["choices"][0]["message"] = {"role": "assistant", "content": answer}
                    self.respond(event)
            else:
                self.send_error(404)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    print(server.server_port, flush=True)
    server.serve_forever()


if __name__ == "__main__":
    main()
