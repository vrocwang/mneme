#!/usr/bin/env python3
"""Media Generation extension for Mneme.

Provides image generation tools via OpenAI DALL-E or Stable Diffusion
compatible API endpoints.

Protocol: stdin/stdout JSON-RPC 2.0 (one message per line).
"""
import json
import os
import sys
import urllib.request
import urllib.error

MANIFEST = {
    "name": "media-generation",
    "version": "0.1.0",
    "description": "Media generation tools (image generation)",
    "tools": [
        {
            "name": "generate_image",
            "description": "Generate an image from a text prompt using DALL-E or Stable Diffusion API. Returns the image URL or local path.",
            "parameters": {
                "type": "object",
                "properties": {
                    "prompt": {
                        "type": "string",
                        "description": "Text description of the image to generate",
                    },
                    "size": {
                        "type": "string",
                        "description": "Image size: '256x256', '512x512', '1024x1024' (default: 1024x1024)",
                    },
                    "model": {
                        "type": "string",
                        "description": "Model to use: 'dall-e-3', 'dall-e-2', or 'stable-diffusion' (default: dall-e-2)",
                    },
                },
                "required": ["prompt"],
            },
        }
    ],
    "agent_defs": [],
    "protocol_min": 1,
}


def generate_image(prompt, size="1024x1024", model="dall-e-2"):
    """Generate an image via OpenAI DALL-E API."""
    api_key = os.environ.get("OPENAI_API_KEY", "")
    endpoint = os.environ.get("OPENAI_API_BASE", "https://api.openai.com/v1")

    if not api_key:
        return {"error": "OPENAI_API_KEY environment variable not set"}

    url = f"{endpoint}/images/generations"
    payload = json.dumps({
        "model": model,
        "prompt": prompt,
        "n": 1,
        "size": size,
    }).encode()

    req = urllib.request.Request(url, data=payload, method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Authorization", f"Bearer {api_key}")

    try:
        with urllib.request.urlopen(req, timeout=60) as resp:
            result = json.loads(resp.read().decode())
            if "data" in result and len(result["data"]) > 0:
                image_url = result["data"][0].get("url", "")
                return {
                    "success": True,
                    "url": image_url,
                    "model": model,
                    "prompt": prompt,
                    "size": size,
                }
            return {"error": "No image in response", "response": result}
    except urllib.error.HTTPError as e:
        body = e.read().decode() if e.fp else ""
        return {"error": f"HTTP {e.code}: {body}"}
    except Exception as e:
        return {"error": str(e)}


def handle_request(req):
    method = req.get("method", "")
    req_id = req.get("id", 0)

    if method == "extension.describe":
        return {"jsonrpc": "2.0", "id": req_id, "result": MANIFEST}
    elif method == "extension.list_tools":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"tools": MANIFEST["tools"]}}
    elif method == "extension.list_agents":
        return {"jsonrpc": "2.0", "id": req_id, "result": {"agents": []}}
    elif method == "extension.call_tool":
        params = req.get("params", {})
        tool_name = params.get("name", "")
        args = params.get("args", {})
        if tool_name == "generate_image":
            result = generate_image(
                prompt=args.get("prompt", ""),
                size=args.get("size", "1024x1024"),
                model=args.get("model", "dall-e-2"),
            )
            return {"jsonrpc": "2.0", "id": req_id, "result": result}
        return {"jsonrpc": "2.0", "id": req_id, "result": {"error": f"unknown tool: {tool_name}"}}
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
