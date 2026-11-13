#!/usr/bin/env python3
"""Generates spec/vectors.json with Python stdlib only (an implementation independent of the Go server).
Every SDK and the server test against that file. Run: python3 -I spec/genvectors.py > spec/vectors.json"""
import hashlib, hmac, json

def msg(key_id, ts, nonce, body): return b"v1." + f"{key_id}.{ts}.{nonce}.".encode() + body
def zero_bits(d):
    n = int.from_bytes(d, "big"); return len(d) * 8 - n.bit_length()
def solve(key_id, ts, body, bits, start=0):
    n = start
    while zero_bits(hashlib.sha256(msg(key_id, ts, str(n), body)).digest()) < bits: n += 1
    return str(n)

secret = "tm_sk_test_secret"
cases = []
for key_id, ts, body, bits in [("k_test", "1763020800", b'{"events":[{"name":"active"}]}', 8),
                               ("k_test", "1763020800", b'{"events":[{"name":"active"}]}', 12),
                               ("k_other", "1700000000", b'{"context":{"v":"1.0"},"events":[{"name":"x","props":{"n":1}}]}', 10)]:
    nonce = solve(key_id, ts, body, bits)
    m = msg(key_id, ts, nonce, body); d = hashlib.sha256(m).digest()
    cases.append({"key_id": key_id, "secret": secret, "ts": ts, "body": body.decode(), "bits": bits, "start": 0,
                  "nonce": nonce, "msg_hex": m.hex(), "sha256_hex": d.hex(), "zero_bits": zero_bits(d),
                  "hmac_hex": hmac.new(secret.encode(), m, hashlib.sha256).hexdigest()})
print(json.dumps({"version": 1,
  "note": "nonce = smallest decimal counter >= start whose sha256(msg) has >= bits leading zero bits; hmac_hex = HMAC-SHA256(secret, msg)",
  "cases": cases}, indent=1))
