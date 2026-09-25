import concurrent.futures
import json
from pathlib import Path
import socket
import subprocess
import time
import urllib.error
import urllib.parse
import urllib.request

namespace = "dsb-hotel"
variant = "hotel-cgpb-es"
output = Path("/users/tomislav/deployments/dsb-hotel/cgpb-es-20260910")
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))


def kubectl_json(*args):
    return json.loads(subprocess.check_output(["kubectl", "-n", namespace, *args, "-o", "json"], text=True))


def get(url):
    with opener.open(url, timeout=20) as response:
        return json.load(response)


service = kubectl_json("get", "service", f"frontend-service-{variant}-ctr")
port = next(p["nodePort"] for p in service["spec"]["ports"] if p["port"] == 2000)
base = f"http://10.10.1.1:{port}"
results = []


def request(method, **args):
    response = get(base + "/" + method + "?" + urllib.parse.urlencode(args))
    results.append({"method": method, "response": response})
    return response["Ret0"]


assert request("UserHandler", username="Cornell_1", password="1111111111") == "Login successful"
try:
    request("UserHandler", username="Cornell_1", password="invalid")
    raise AssertionError("invalid login accepted")
except urllib.error.HTTPError as error:
    assert error.code == 500 and "Invalid Credentials" in error.read().decode()
for criterion in ("dis", "rate", "price"):
    assert request("RecommendHandler", lat=37.7835, lon=-122.41, require=criterion, locale="en")


def search(_):
    return request("SearchHandler", customerName="KubernetesSmoke", inDate="2015-04-09",
                   outDate="2015-04-10", lat=37.7835, lon=-122.41, locale="en")


assert search(0)
with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
    searches = list(executor.map(search, range(32)))
assert all(searches)
assert request("ReservationHandler", inDate="2015-04-09", outDate="2015-04-10",
               hotelId="1", customerName="KubernetesSmoke", username="Cornell_1",
               password="1111111111", roomNumber=1) == "Reservation successful"
assert search(0)
print("NodePort API checks passed, including 32 concurrent searches and a reservation", flush=True)

with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    jaeger_port = sock.getsockname()[1]
with (output / "jaeger-port-forward.log").open("w") as log:
    forward = subprocess.Popen(["kubectl", "-n", namespace, "port-forward",
                                f"service/jaeger-{variant}-ctr", f"{jaeger_port}:16686"],
                               stdout=log, stderr=subprocess.STDOUT)
    try:
        deadline = time.monotonic() + 60
        names = []
        while time.monotonic() < deadline:
            if forward.poll() is not None:
                raise RuntimeError("Jaeger port-forward exited")
            try:
                names = get(f"http://127.0.0.1:{jaeger_port}/api/services")["data"]
                if len(names) >= 8:
                    break
            except OSError:
                pass
            time.sleep(.5)
        expected = ("frontend", "search", "geo", "rate", "profile", "recomd", "user", "reserv")
        for name in expected:
            assert any(name + "_service_" in service or name + "-service-" in service for service in names), names
        frontend = next(name for name in names if "frontend" in name)
        traces = get(f"http://127.0.0.1:{jaeger_port}/api/traces?" + urllib.parse.urlencode({
            "service": frontend, "limit": 100, "lookback": "1h",
        }))["data"]
        assert traces
        assert any(len(trace["processes"]) >= 6 for trace in traces), "no distributed search trace"
        (output / "verification-traces.json").write_text(json.dumps(traces, indent=2))
        print(f"Jaeger reports {len(names)} hotel services and {len(traces)} frontend traces", flush=True)
    finally:
        forward.terminate()
        forward.wait(timeout=10)

pods = kubectl_json("get", "pods")
assert pods["items"]
for pod in pods["items"]:
    assert pod["status"]["phase"] == "Running", pod["metadata"]["name"]
    assert all(status["ready"] for status in pod["status"]["containerStatuses"]), pod["metadata"]["name"]
images = {status["imageID"] for pod in pods["items"] for status in pod["status"]["containerStatuses"]}
assert all("@sha256:" in image for image in images), images
apps = [pod for pod in pods["items"] if "-service-" in pod["metadata"]["name"]]
assert len(apps) == 8
assert len({pod["spec"]["nodeName"] for pod in apps}) == 8
collectors = {pod["spec"]["nodeName"] for pod in pods["items"] if pod["metadata"]["name"].startswith("otelcol-")}
assert {pod["spec"]["nodeName"] for pod in apps} <= collectors
assert kubectl_json("get", "service", f"otelcol-{variant}-ctr")["spec"]["internalTrafficPolicy"] == "Local"
summary = {
    "namespace": namespace, "frontend_url": base, "ready_pods": len(pods["items"]),
    "services_on_distinct_nodes": len(apps), "collector_pods": len(collectors),
    "successful_api_requests": len(results), "concurrent_searches": len(searches),
    "jaeger_services": names, "frontend_traces": len(traces),
}
(output / "verification.json").write_text(json.dumps(summary, indent=2) + "\n")
(output / "verification-responses.json").write_text(json.dumps(results, indent=2) + "\n")
(output / "verification-pods.json").write_text(json.dumps(pods, indent=2) + "\n")
print(json.dumps(summary, indent=2), flush=True)
