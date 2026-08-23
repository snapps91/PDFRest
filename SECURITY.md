# Security Policy

Security is taken seriously in **PDFRest**.

PDFRest processes arbitrary HTML using a Chromium-based rendering environment. Because of this, deployments should follow the security recommendations described below.

## Supported Versions

Security fixes are provided for the latest released version of PDFRest.

| Version | Supported |
| ------- | --------- |
| 1.3.x   | ✅         |
| < 1.3   | ❌         |

Users are strongly encouraged to keep PDFRest and the Chromium version included in the container image up to date.

## Reporting a Vulnerability

Please **do not report security vulnerabilities through public GitHub Issues, Discussions, or Pull Requests**.

Instead, report vulnerabilities privately using **GitHub Security Advisories**:

1. Open the **Security** tab of this repository.
2. Select **Report a vulnerability**.
3. Provide as much information as possible about the issue.

A useful report should include:

* a description of the vulnerability;
* the affected PDFRest version;
* the deployment configuration, if relevant;
* steps required to reproduce the issue;
* a proof of concept, when available;
* the expected security impact;
* any suggested mitigation or fix.

Please avoid including sensitive information that is not required to reproduce the issue.

## Response Process

After receiving a security report, the maintainers will attempt to:

1. acknowledge the report;
2. reproduce and assess the vulnerability;
3. determine its severity and affected versions;
4. prepare a fix or mitigation where appropriate;
5. publish a patched release;
6. coordinate disclosure with the reporter when necessary.

Response times may vary depending on the complexity and severity of the issue.

## Security Model

PDFRest is designed to operate as an **internal rendering service** behind a trusted application or API layer.

It intentionally does **not** provide:

* authentication;
* authorization;
* tenant isolation;
* rate limiting intended for Internet-facing abuse protection;
* TLS termination;
* user or API-key management.

These controls should be implemented by the surrounding infrastructure or application.

PDFRest should therefore **not be exposed directly to the public Internet** unless appropriate security controls are placed in front of it.

Typical deployments should place PDFRest behind components such as:

* an authenticated backend service;
* an API gateway;
* a reverse proxy;
* Kubernetes NetworkPolicies;
* firewall rules;
* private networking;
* service-to-service authentication.

Lack of authentication in PDFRest itself is considered part of its documented architecture and is therefore **not considered a security vulnerability**.

## Untrusted HTML

PDFRest accepts HTML and renders it using Chromium.

HTML submitted to the service must therefore be considered potentially hostile when it originates from untrusted users.

Depending on the supplied content and the Chromium environment, HTML may attempt to load external or internal resources such as:

```text
https://example.com/image.png
http://internal-service/
http://169.254.169.254/
file://...
```

Operators accepting untrusted HTML should implement network-level isolation appropriate for their environment.

Recommended protections include:

* restricting outbound network access from the PDFRest container;
* preventing access to cloud instance metadata endpoints;
* preventing access to internal infrastructure that the renderer does not require;
* running the service in an isolated network segment;
* using Kubernetes NetworkPolicies or equivalent firewall controls;
* applying CPU, memory, request-size and execution-time limits.

Applications should not assume that HTML is safe simply because its only intended output is a PDF.

## Server-Side Request Forgery

Because Chromium may retrieve resources referenced by supplied HTML, deployments accepting untrusted HTML may be susceptible to **Server-Side Request Forgery (SSRF)** depending on their network configuration.

PDFRest cannot determine which network resources are considered sensitive within a particular deployment.

Preventing Chromium from reaching sensitive services is therefore primarily a deployment responsibility.

However, vulnerabilities that allow bypassing explicit security restrictions implemented by PDFRest itself are considered valid security reports.

## Chromium Security

PDFRest relies on Chromium for HTML rendering.

Vulnerabilities originating exclusively from Chromium should generally be reported to the Chromium project.

However, please report the issue to PDFRest if:

* PDFRest configures Chromium in a way that introduces an additional vulnerability;
* PDFRest bypasses a Chromium security mechanism;
* the container ships a known vulnerable Chromium version after an appropriate fixed version is available;
* the interaction between PDFRest and Chromium creates a vulnerability specific to PDFRest.

Keeping the PDFRest container image updated is strongly recommended so that current Chromium security updates are included.

## Container Security

When running the official container image, operators should apply standard container security practices.

Where supported by the deployment environment:

* avoid running privileged containers;
* avoid mounting sensitive host directories;
* avoid mounting the Docker or container runtime socket;
* use a read-only root filesystem where practical;
* drop unnecessary Linux capabilities;
* enforce memory and CPU limits;
* isolate the container network;
* avoid exposing the Chromium DevTools endpoint outside the trusted environment.

In particular, the Chromium DevTools Protocol endpoint provides powerful control over the browser and must be treated as a trusted internal interface.

It should never be exposed publicly.

## Remote Chromium

PDFRest supports connecting to an externally managed Chromium instance using `CHROME_ENDPOINT` or `CHROME_WS`.

When using this configuration, operators are responsible for securing communication between PDFRest and Chromium.

The remote debugging endpoint or WebSocket should only be reachable from trusted services.

Exposing a Chromium DevTools endpoint to an untrusted network can result in complete control over the browser process.

## Resource Exhaustion

Rendering HTML and generating PDFs are computationally expensive operations.

Malicious or pathological input may consume significant:

* CPU;
* memory;
* network bandwidth;
* rendering time.

PDFRest provides request timeouts and body-size limits, but infrastructure-level protections are still recommended.

Deployments exposed to untrusted clients should implement appropriate:

* authentication;
* rate limiting;
* concurrency limits;
* CPU limits;
* memory limits;
* request limits.

Denial-of-service reports are considered security issues when they demonstrate a practical method of bypassing existing protections or causing disproportionate resource consumption beyond the expected cost of PDF rendering.

## Secrets

PDFRest should not require application secrets for normal operation.

Do not include credentials, API keys, tokens, private certificates, or other sensitive information in:

* HTML sent to the renderer;
* container images;
* environment variables unless strictly required by the deployment;
* public bug reports;
* logs.

If credentials are required for external resources used during rendering, they should be managed using the secret-management facilities provided by the deployment environment.

## Out of Scope

The following are generally not considered vulnerabilities in PDFRest:

* the absence of authentication or authorization;
* publicly exposing PDFRest despite the documented deployment model;
* SSRF caused solely by unrestricted network access granted to untrusted HTML;
* denial of service caused by intentionally providing unlimited resources or unrestricted public access;
* vulnerabilities affecting unsupported versions;
* vulnerabilities exclusively affecting Chromium without a PDFRest-specific impact;
* vulnerabilities requiring an already-compromised host or container runtime;
* social engineering or phishing;
* theoretical issues without a realistic attack scenario.

Reports demonstrating an unexpected bypass of a documented protection are still welcome.

## Responsible Disclosure

Please allow maintainers a reasonable opportunity to investigate and address confirmed vulnerabilities before publicly disclosing them.

We appreciate researchers who act in good faith to improve the security of PDFRest and its users.
