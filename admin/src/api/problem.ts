import type { Problem } from "./admin-api";

export class ApiProblem extends Error {
  readonly status: number;
  readonly title: string;
  readonly code: string;
  readonly requestId: string;
  readonly type: string;

  constructor(
    status: number,
    code: string,
    requestId: string,
    title: string,
    type = "about:blank",
  ) {
    super(title);
    this.name = "ApiProblem";
    this.status = status;
    this.title = title;
    this.code = code;
    this.requestId = requestId;
    this.type = type;
  }
}

export function invalidApiResponse(message = "Invalid API response"): ApiProblem {
  return new ApiProblem(502, "invalid_api_response", "client", message);
}

export function networkProblem(): ApiProblem {
  return new ApiProblem(503, "network_error", "client", "Network request failed");
}

export function isProblem(value: unknown): value is Problem {
  if (typeof value !== "object" || value === null) {
    return false;
  }
  const candidate = value as Record<string, unknown>;
  return typeof candidate.type === "string"
    && typeof candidate.title === "string"
    && typeof candidate.status === "number"
    && Number.isInteger(candidate.status)
    && typeof candidate.code === "string"
    && typeof candidate.requestId === "string";
}
