#!/usr/bin/env python3
"""Vision Agent extension for Mneme.

Provides a vision understanding agent that analyzes images and
screenshots using vision models.

Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
"""
import json
import sys

MANIFEST = {
    "name": "vision-agent",
    "version": "0.1.0",
    "description": "Vision understanding agent",
    "tools": [],
    "agent_defs": ["vision_agent"],
    "protocol_min": 1,
}

AGENTS = [
    {
        "id": "vision_agent",
        "name": "Vision Agent",
        "description": "Analyzes images and screenshots using vision models. Can describe visual content, identify objects, and extract text from images.",
        "tier": "reasoning",
        "systemPrompt": "You are a Vision Agent specialized in analyzing and describing images. You can identify objects, extract text, describe scenes, and answer questions about visual content.",
        "toolAllowlist": ["read_file", "list_dir", "image_info", "browser", "current_time"],
        "maxIterations": 10,
        "hidden": False,
    }
]


def handle_request(req):
    method = req.get("method", "")
    req_id = req.get("id", 0)

    if method == "extension.describe":
        return {"jsonrpc": "2.0", "id": req_id, "result": MANIFEST}
    elif method == "extension.list_tools":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": []}}
    elif method == "extension.list_agents":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"agents": AGENTS}}
    elif method == "extension.call_tool":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"error": "no tools available"}}
    else:
        return {"jsonrpc": "2.0", "id": req_id, "error": {"code": -32601, "message": f"unknown method: {method}"}}


def main():
    for line in sys.stdin:
        line = line.strip()
        if not line:
            continue
        try:
            req = json.loads(line)
            resp = handle_request(req)
            sys.stdout.write(json.dumps(resp) + "\n")
            sys.stdout.flush()
        except json.JSONDecodeError:
            pass


if __name__ == "__main__":
    main()
