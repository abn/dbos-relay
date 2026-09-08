import os
import sys
import json
import base64
import hashlib
import socket
import struct
import time
from datetime import datetime, timezone

app_name = os.environ.get("DBOS_APP_NAME", "python-sample-app")
relay_url = os.environ.get("RELAY_URL", "http://localhost:8090")
api_key = os.environ.get("RELAY_API_KEY", "test-key")
exec_id = f"exec-python-{os.urandom(4).hex()}"

# Standard DBOS SDK launch log
now_iso = datetime.now(timezone.utc).isoformat()
print(f'time={now_iso} level=INFO msg="DBOS launched" app_version=v1.0.0 executor_id={exec_id} language=python', flush=True)

def connect_ws():
    # Parse relay_url
    url = relay_url
    if "://" in url:
        url = url.split("://", 1)[1]
    if ":" in url:
        host, port = url.split(":", 1)
        port = int(port.split("/")[0])
    else:
        host, port = url.split("/")[0], 8090

    path = f"/websocket/{app_name}/{api_key}"
    s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    s.connect((host, port))

    sec_key = base64.b64encode(os.urandom(16)).decode("ascii")
    handshake = (
        f"GET {path} HTTP/1.1\r\n"
        f"Host: {host}:{port}\r\n"
        f"Upgrade: websocket\r\n"
        f"Connection: Upgrade\r\n"
        f"Sec-WebSocket-Key: {sec_key}\r\n"
        f"Sec-WebSocket-Version: 13\r\n\r\n"
    )
    s.sendall(handshake.encode("utf-8"))

    # Read handshake response
    resp = b""
    while b"\r\n\r\n" not in resp:
        chunk = s.recv(1024)
        if not chunk:
            break
        resp += chunk

    if b"101 Switching Protocols" not in resp:
        print(f"Handshake failed: {resp[:100]}", file=sys.stderr)
        return

    def send_frame(payload_bytes, opcode=0x1):
        length = len(payload_bytes)
        mask_key = os.urandom(4)
        header = bytearray([0x80 | opcode])
        if length < 126:
            header.append(0x80 | length)
        elif length < 65536:
            header.append(0x80 | 126)
            header.extend(struct.pack("!H", length))
        else:
            header.append(0x80 | 127)
            header.extend(struct.pack("!Q", length))
        header.extend(mask_key)
        masked = bytearray(payload_bytes)
        for i in range(length):
            masked[i] ^= mask_key[i % 4]
        s.sendall(header + masked)

    def recv_frame():
        header = s.recv(2)
        if len(header) < 2:
            return None, None
        b1, b2 = header[0], header[1]
        opcode = b1 & 0x0F
        masked = (b2 & 0x80) != 0
        length = b2 & 0x7F
        if length == 126:
            length = struct.unpack("!H", s.recv(2))[0]
        elif length == 127:
            length = struct.unpack("!Q", s.recv(8))[0]
        mask_key = s.recv(4) if masked else b""
        payload = b""
        while len(payload) < length:
            chunk = s.recv(length - len(payload))
            if not chunk:
                break
            payload += chunk
        if masked:
            unmasked = bytearray(payload)
            for i in range(len(unmasked)):
                unmasked[i] ^= mask_key[i % 4]
            payload = bytes(unmasked)
        return opcode, payload

    while True:
        try:
            opcode, payload = recv_frame()
            if opcode is None:
                break
            if opcode == 0x8: # Close
                break
            if opcode == 0x9: # Ping
                send_frame(payload, opcode=0xA)
                continue
            if opcode == 0x1: # Text frame
                msg = json.loads(payload.decode("utf-8"))
                mtype = msg.get("type")
                req_id = msg.get("request_id")
                if mtype == "executor_info":
                    reply = {
                        "type": "executor_info",
                        "request_id": req_id,
                        "executor_id": exec_id,
                        "app_version": "v1.0.0",
                        "language": "python",
                        "dbos_version": "0.1.0",
                        "hostname": "localhost"
                    }
                    send_frame(json.dumps(reply).encode("utf-8"))
                elif mtype == "get_workflow":
                    reply = {
                        "type": "get_workflow",
                        "request_id": req_id,
                        "output": {
                            "WorkflowUUID": msg.get("workflow_id", "wf-sample"),
                            "Status": "SUCCESS",
                            "WorkflowName": "hello_workflow",
                            "ApplicationVersion": "v1.0.0",
                            "Output": json.dumps({"result": "python-output"})
                        }
                    }
                    send_frame(json.dumps(reply).encode("utf-8"))
                elif mtype == "list_steps":
                    reply = {
                        "type": "list_steps",
                        "request_id": req_id,
                        "steps": [
                            {
                                "function_id": 1,
                                "function_name": "step1",
                                "output": json.dumps({"status": "step-done"})
                            }
                        ]
                    }
                    send_frame(json.dumps(reply).encode("utf-8"))
        except Exception as e:
            print(f"Connection loop exception: {e}", file=sys.stderr)
            break

if __name__ == "__main__":
    while True:
        try:
            connect_ws()
        except Exception as e:
            print(f"connect_ws error: {e}", file=sys.stderr, flush=True)
            time.sleep(1)
