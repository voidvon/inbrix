#!/usr/bin/env python3
"""inbrix product-library API helper (read + safe update).

Usage:
  python3 inbrix_api.py whoami                 # capabilities / auth smoke test
  python3 inbrix_api.py list                   # products: name | id | version | category
  python3 inbrix_api.py categories             # category tree with counts
  python3 inbrix_api.py dimensions             # shared dimension dictionary
  python3 inbrix_api.py show <productId|名称子串>  # full product JSON
  python3 inbrix_api.py coverage               # price coverage across the library

Import from import scripts:
  from inbrix_api import api, get_all_products, get_product, update_product

update_product() always sends name/brand/model/categoryId/specification
(the PUT replaces every field — omitting categoryId uncategorizes the
product), retries once on 409 version conflicts, and can preserve existing
variant/price ids when only patching the catalog.
"""
import json
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

def _env():
    env_path = Path(__file__).resolve()
    for parent in env_path.parents:
        candidate = parent / ".env"
        if candidate.exists():
            env = {}
            for line in candidate.read_text(encoding="utf-8").splitlines():
                line = line.strip()
                if line and not line.startswith("#") and "=" in line:
                    k, v = line.split("=", 1)
                    env[k.strip()] = v.strip()
            if "API_URL" in env and "API_KEY" in env:
                return env
    raise SystemExit("inbrix_api: no .env with API_URL/API_KEY found in any parent directory")

ENV = _env()
BASE = ENV["API_URL"].rstrip("/")
KEY = ENV["API_KEY"]

def api(method, path, payload=None, retries=1):
    """Call the JSON API; returns (status, parsed-body-or-error-text)."""
    data = json.dumps(payload).encode("utf-8") if payload is not None else None
    req = urllib.request.Request(BASE + path, data=data, method=method)
    req.add_header("Authorization", "Bearer " + KEY)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            body = resp.read().decode("utf-8")
            return resp.status, json.loads(body) if body else {}
    except urllib.error.HTTPError as e:
        return e.code, e.read().decode("utf-8", "replace")

def get_all_products():
    """Return {name: product} for the whole library (search list form)."""
    out, offset = {}, 0
    while True:
        status, page = api("GET", "/products?limit=20&offset=%d" % offset)
        if status != 200:
            raise RuntimeError("list products failed: %s %s" % (status, page))
        for p in page.get("products", []):
            out[p["name"]] = p
        if not page.get("hasMore"):
            return out
        offset = page["nextOffset"]

def get_product(pid):
    status, full = api("GET", "/products/%s" % pid)
    if status != 200:
        raise RuntimeError("get product failed: %s %s" % (status, full))
    return full["product"]

def update_product(pid, *, name, brand, model, categoryId, specification,
                   catalog=None, version=None):
    """PUT with every scalar field always present. catalog=None keeps the
    existing catalog. Retries once after refetching on a 409 conflict."""
    def attempt(ver):
        payload = {"name": name, "brand": brand, "model": model,
                   "categoryId": categoryId, "specification": specification,
                   "version": ver}
        if catalog is not None:
            payload["catalog"] = catalog
        return api("PUT", "/products/%s" % pid, payload)
    if version is None:
        version = get_product(pid)["version"]
    status, resp = attempt(version)
    if status == 409:
        time.sleep(0.3)
        latest = get_product(pid)
        status, resp = attempt(latest["version"])
    if status >= 400:
        raise RuntimeError("update failed: %s %s" % (status, resp))
    return resp["product"]

def patch_dimension(pid, key, definition, value):
    """Add a dimension (e.g. price_year) to a product in place, preserving
    all existing variant and price ids. Returns the updated product."""
    cur = get_product(pid)
    cat = cur.get("catalog")
    if not cat:
        raise RuntimeError("product %s has no catalog" % pid)
    dims = cat.get("dimensions", [])
    if not any(d.get("key") == key for d in dims):
        dims = dims + [definition]
    variants = []
    for v in cat.get("variants", []):
        attrs = dict(v.get("attributes") or {})
        attrs[key] = value
        nv = {"sku": v["sku"], "attributes": attrs, "prices": v.get("prices") or []}
        if v.get("id"):
            nv["id"] = v["id"]
        variants.append(nv)
    return update_product(pid, name=cur["name"], brand=cur["brand"],
                          model=cur["model"], categoryId=cur.get("categoryId", ""),
                          specification=cur.get("specification", ""),
                          catalog={"dimensions": dims, "variants": variants},
                          version=cur["version"])

# --------------------------------------------------------------------- CLI
def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "whoami"
    if cmd == "whoami":
        status, caps = api("GET", "/capabilities")
        print("API:", BASE, "| status:", status)
        print(json.dumps(caps, ensure_ascii=False, indent=2))
        sys.exit(0 if status == 200 else 1)
    if cmd == "list":
        for name, p in sorted(get_all_products().items()):
            print("%-40s %s  v%d  %s" % (name[:40], p["id"], p["version"], p["categoryName"]))
        return
    if cmd == "categories":
        status, resp = api("GET", "/product-categories")
        cats = {c["id"]: c for c in resp["categories"]}
        for c in sorted(cats.values(), key=lambda x: x["name"]):
            parent = cats.get(c["parentId"], {}).get("name", "")
            print("%s%s (%d)" % ("  " if parent else "", c["name"], c["productCount"]))
        return
    if cmd == "dimensions":
        status, resp = api("GET", "/product-dimensions")
        for d in sorted(resp["dimensions"], key=lambda x: x["label"]):
            print("%-18s %-14s %-8s %-6s products=%d" % (d["key"], d["label"], d["type"], d["unit"], d["productCount"]))
        return
    if cmd == "show":
        needle = sys.argv[2]
        prods = get_all_products()
        pid = needle if needle in prods and prods[needle]["id"] == needle else None
        if pid is None:
            for name, p in prods.items():
                if needle in name or needle == p["id"]:
                    pid = p["id"]
                    break
        if pid is None:
            raise SystemExit("no product matches %r" % needle)
        print(json.dumps(get_product(pid), ensure_ascii=False, indent=2))
        return
    if cmd == "coverage":
        total_v = total_p = bad = 0
        for name, p in sorted(get_all_products().items()):
            cat = (get_product(p["id"]).get("catalog") or {})
            vs = cat.get("variants", [])
            priced = [v for v in vs if v.get("prices")]
            bad += len([pr for v in priced for pr in v["prices"]
                        if pr.get("taxRate") or pr.get("taxMode") or pr.get("validFrom") or pr.get("source")])
            total_v += len(vs); total_p += len(priced)
            print("%-40s 变体%-3d 有价%-3d" % (name[:40], len(vs), len(priced)))
        print("\n总变体 %d | 总价格 %d | 带条件字段 %d" % (total_v, total_p, bad))
        return
    raise SystemExit("unknown command: %s" % cmd)

if __name__ == "__main__":
    main()
