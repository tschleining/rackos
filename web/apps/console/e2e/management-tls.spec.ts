
import {
  expect,
  test,
} from "@playwright/test";

test(
  "management console uses verified HTTPS",
  async ({ page, request }) => {
    test.skip(
      process.env.RACKOS_E2E_HTTPS !== "true",
      "Requires the isolated HTTPS test launcher",
    );

    const response = await page.goto("/");

    expect(response).not.toBeNull();
    expect(response?.status()).toBe(200);

    expect(
      new URL(page.url()).protocol,
    ).toBe("https:");

    const securityDetails =
      await response!.securityDetails();

    expect(securityDetails).not.toBeNull();

    expect(securityDetails?.protocol)
      .toBe("TLS 1.3");

    expect(securityDetails?.issuer)
      .toContain("RackOS Management CA");

    expect(securityDetails?.subjectName)
      .toBe("RackOS Management HTTPS");

    await expect(
      page.getByRole("heading", {
        name: "Overview",
        exact: true,
      }),
    ).toBeVisible();

    const health = await request.get(
      "/healthz",
    );

    expect(health.status()).toBe(200);

    expect(await health.json()).toEqual({
      status: "healthy",
    });

    const wrongOrigin = await request.get(
      "/healthz",
      {
        headers: {
          Origin: "http://127.0.0.1:18780",
        },
      },
    );

    expect(wrongOrigin.status()).toBe(403);

    const correctOrigin = await request.get(
      "/healthz",
      {
        headers: {
          Origin: "https://127.0.0.1:18780",
          "Sec-Fetch-Site": "same-origin",
        },
      },
    );

    expect(correctOrigin.status()).toBe(200);
  },
);
