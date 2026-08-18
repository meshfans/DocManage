import { expect, test } from "@playwright/test";

const backendURL = process.env.E2E_BACKEND_URL ?? "http://localhost:8090";
const prometheusURL = process.env.E2E_PROMETHEUS_URL ?? "http://localhost:19090";

test.describe("Docker 可观测性端点", () => {
  test("后端存活和就绪检查符合契约", async ({ request }) => {
    const health = await request.get(`${backendURL}/healthz`);
    expect(health.status()).toBe(200);
    await expect(health.json()).resolves.toMatchObject({
      service: "doc-server",
      status: "alive",
      version: "1.0.0"
    });

    const ready = await request.get(`${backendURL}/readyz`);
    expect(ready.status()).toBe(200);
    await expect(ready.json()).resolves.toEqual({
      status: "ready",
      checks: {
        db: "ok",
        maintenance: "ok",
        scheduler: "ok",
        ws_hub: "ok"
      }
    });
  });

  test("后端输出 Prometheus 指标", async ({ request }) => {
    const response = await request.get(`${backendURL}/metrics`);
    expect(response.status()).toBe(200);
    expect(response.headers()["content-type"]).toContain("text/plain");

    const metrics = await response.text();
    for (const metric of [
      "business_events_total",
      "db_open_connections",
      "scheduler_running",
      "ws_clients_connected",
      "http_requests_total"
    ]) {
      expect(metrics).toContain(metric);
    }
  });

  test("Prometheus 已就绪并成功抓取后端", async ({ request }) => {
    const ready = await request.get(`${prometheusURL}/-/ready`);
    expect(ready.status()).toBe(200);
    expect(await ready.text()).toContain("Prometheus Server is Ready");

    await expect
      .poll(
        async () => {
          const targets = await request.get(`${prometheusURL}/api/v1/targets`);
          expect(targets.status()).toBe(200);
          const body = await targets.json();
          expect(body.status).toBe("success");
          return body.data.activeTargets.some(
            (target: {
              health?: string;
              lastError?: string;
              labels?: { job?: string };
            }) =>
              target.health === "up" &&
              target.lastError === "" &&
              target.labels?.job === "doc-server"
          );
        },
        { timeout: 30_000 }
      )
      .toBe(true);
  });
});
