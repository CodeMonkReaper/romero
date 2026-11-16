"""Lightweight HTTP sidecar serving DeBERTa NLI inference for ROMERO."""

from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os
import threading
from typing import Any, Callable, Dict, List


DEFAULT_MODEL = "MoritzLaurer/mDeBERTa-v3-base-mnli-xnli"


class TransformersNLIPredictor:
    """Lazy-loading HuggingFace sequence-classification NLI predictor."""

    def __init__(self, model_id: str, device: str = "cpu") -> None:
        self.model_id = model_id
        self.device = device
        self._lock = threading.Lock()
        self._tokenizer: Any = None
        self._model: Any = None
        self._id2label: Dict[int, str] = {}

    def _ensure_loaded(self) -> None:
        if self._model is not None:
            return
        with self._lock:
            if self._model is not None:
                return
            import torch
            from transformers import AutoModelForSequenceClassification, AutoTokenizer

            cache_dir = os.environ.get("HF_HOME") or os.environ.get("TRANSFORMERS_CACHE") or "/data"
            self._tokenizer = AutoTokenizer.from_pretrained(self.model_id, cache_dir=cache_dir)
            model = AutoModelForSequenceClassification.from_pretrained(
                self.model_id, cache_dir=cache_dir
            )
            if self.device != "cpu" and torch.cuda.is_available():
                model = model.to(self.device)
            model.eval()
            self._id2label = {
                int(k): str(v).lower() for k, v in (model.config.id2label or {}).items()
            }
            self._model = model

    def predict(self, pairs: List[Dict[str, str]]) -> List[Dict[str, float]]:
        self._ensure_loaded()
        import torch

        premises = [p.get("premise", "") for p in pairs]
        hypotheses = [p.get("hypothesis", "") for p in pairs]
        enc = self._tokenizer(
            premises,
            hypotheses,
            padding=True,
            truncation=True,
            max_length=512,
            return_tensors="pt",
        )
        if self.device != "cpu" and torch.cuda.is_available():
            enc = {k: v.to(self.device) for k, v in enc.items()}

        with torch.no_grad():
            logits = self._model(**enc).logits
            probs = torch.softmax(logits, dim=-1).cpu().tolist()

        scores: List[Dict[str, float]] = []
        for row in probs:
            item = {"entailment": 0.0, "neutral": 0.0, "contradiction": 0.0}
            for idx, val in enumerate(row):
                label = self._id2label.get(idx, "")
                if "entail" in label:
                    item["entailment"] = float(val)
                elif "contradict" in label:
                    item["contradiction"] = float(val)
                elif "neutral" in label:
                    item["neutral"] = float(val)
            scores.append(item)
        return scores


def make_handler(predictor_fn: Callable[[List[Dict[str, str]]], List[Dict[str, float]]]):
    class NLIHandler(BaseHTTPRequestHandler):
        def log_message(self, format: str, *args: Any) -> None:  # noqa: A002
            return

        def _send_json(self, status: int, body: Dict[str, Any]) -> None:
            raw = json.dumps(body).encode("utf-8")
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(raw)))
            self.end_headers()
            self.wfile.write(raw)

        def do_GET(self) -> None:  # noqa: N802
            if self.path == "/health":
                self._send_json(200, {"status": "ok"})
                return
            self._send_json(404, {"error": "not found"})

        def do_POST(self) -> None:  # noqa: N802
            if self.path != "/nli":
                self._send_json(404, {"error": "not found"})
                return
            try:
                length = int(self.headers.get("Content-Length", "0") or "0")
            except ValueError:
                self._send_json(400, {"error": "invalid content length"})
                return
            if length <= 0 or length > 4 * 1024 * 1024:
                self._send_json(400, {"error": "invalid content length"})
                return
            try:
                payload = json.loads(self.rfile.read(length).decode("utf-8"))
            except (UnicodeDecodeError, json.JSONDecodeError):
                self._send_json(400, {"error": "invalid json"})
                return
            if not isinstance(payload, dict):
                self._send_json(400, {"error": "payload must be a json object"})
                return
            pairs = payload.get("pairs")
            if not isinstance(pairs, list):
                self._send_json(400, {"error": "pairs must be a list"})
                return
            if len(pairs) == 0:
                self._send_json(200, {"scores": []})
                return
            scores = predictor_fn(pairs)
            self._send_json(200, {"scores": scores})

    return NLIHandler


def main() -> None:
    host = os.environ.get("NLI_HOST", "0.0.0.0")
    port = int(os.environ.get("NLI_PORT", "8083"))
    model_id = os.environ.get("NLI_MODEL_ID", DEFAULT_MODEL)
    device = os.environ.get("NLI_DEVICE", "cpu")
    predictor = TransformersNLIPredictor(model_id=model_id, device=device)
    if os.environ.get("NLI_PRELOAD", "false").lower() in ("1", "true", "yes"):
        predictor._ensure_loaded()
    srv = ThreadingHTTPServer((host, port), make_handler(predictor.predict))
    srv.serve_forever()


if __name__ == "__main__":
    main()
