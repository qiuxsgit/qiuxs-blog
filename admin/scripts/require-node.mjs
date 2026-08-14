const requiredVersion = "v20.19.4";

export function assertNodeVersion(version) {
  if (version !== requiredVersion) {
    throw new Error(`Node 20.19.4 required, got ${version}`);
  }
}

assertNodeVersion(process.version);
