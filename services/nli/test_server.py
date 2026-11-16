"""Unit tests for ROMERO NLI HTTP sidecar contract."""

from http.server import ThreadingHTTPServer
import json
import threading
import unittest
from urllib.error import HTTPError
from urllib.request import Request, urlopen

from server import make_handler


class TestNLIServer(unittest.TestCase):
    def setUp(self) -> None:
        def fake_predict(pairs):
            out = []
            for p in pairs:
                if "5433" in p.get("hypothesis", ""):
                    out.append({"entailment": 0.02, "neutral": 0.06, "contradiction": 0.92})
                else:
                    out.append({"entailment": 0.91, "neutral": 0.07, "contradiction": 0.02})
            return out

        self.srv = ThreadingHTTPServer(("127.0.0.1", 0), make_handler(fake_predict))
        self.port = self.srv.server_address[1]
        self.thread = threading.Thread(target=self.srv.serve_forever, daemon=True)
        self.thread.start()

    def tearDown(self) -> None:
        self.srv.shutdown()
        self.srv.server_close()

    def test_health_and_nli_contract(self) -> None:
        with urlopen(f"http://127.0.0.1:{self.port}/health") as resp:
            self.assertEqual(resp.status, 200)
            self.assertEqual(json.loads(resp.read().decode("utf-8"))["status"], "ok")

        body = json.dumps({
            "model": "MoritzLaurer/mDeBERTa-v3-base-mnli-xnli",
            "pairs": [
                {"premise": "Postgres port is 5432", "hypothesis": "Postgres port is 5433"},
                {"premise": "Use CGO_ENABLED=0", "hypothesis": "Build with CGO_ENABLED=0"},
            ],
        }).encode("utf-8")
        req = Request(
            f"http://127.0.0.1:{self.port}/nli",
            data=body,
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with urlopen(req) as resp:
            self.assertEqual(resp.status, 200)
            parsed = json.loads(resp.read().decode("utf-8"))
        self.assertEqual(len(parsed["scores"]), 2)
        self.assertGreater(parsed["scores"][0]["contradiction"], 0.9)
        self.assertGreater(parsed["scores"][1]["entailment"], 0.9)

    def test_rejects_invalid_payload(self) -> None:
        req = Request(
            f"http://127.0.0.1:{self.port}/nli",
            data=b'{"pairs": "not-a-list"}',
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(HTTPError) as ctx:
            urlopen(req)
        self.assertEqual(ctx.exception.code, 400)

        req_arr = Request(
            f"http://127.0.0.1:{self.port}/nli",
            data=b"[]",
            headers={"Content-Type": "application/json"},
            method="POST",
        )
        with self.assertRaises(HTTPError) as ctx_arr:
            urlopen(req_arr)
        self.assertEqual(ctx_arr.exception.code, 400)


if __name__ == "__main__":
    unittest.main()
