import { load } from "js-yaml";
import { execFileSync, spawnSync } from "node:child_process";
import {
  chmodSync,
  existsSync,
  mkdirSync,
  mkdtempSync,
  readFileSync,
  rmSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, describe, expect, test } from "vitest";

const formaScriptPath = fileURLToPath(new URL("../forma.sh", import.meta.url));
const dockerComposeTemplatePath = fileURLToPath(new URL("../docker-compose.yml", import.meta.url));
const legacyValkeyImage =
  "valkey/valkey@sha256:12ba4f45a7c3e1d0f076acd616cb230834e75a77e8516dde382720af32832d6d";
const multiArchValkeyImage =
  "valkey/valkey@sha256:e0eb7c480958d32bdc4357a74bdd70653ae15f2f9b4c93c4a5a9fad1dc471c84";

const tempDirs: string[] = [];
const dockerComposeOverrideKeys = [
  "POSTGRES_PASSWORD",
  "POSTGRES_PASSWORD_URL_ENCODED",
  "AUTHZED_TOKEN",
  "AUTHZED_DATABASE_PASSWORD",
  "FORMA_UNSET_PASSWORD_SENTINEL",
];
const dockerComposeTestTimeout = 30_000;

const dockerComposeTest = (name: string, testFunction: () => void): void => {
  test(name, testFunction, dockerComposeTestTimeout);
};

type RenderedDockerComposeConfig = {
  services: Record<string, { environment?: Record<string, string> }>;
};

const createTempDir = (): string => {
  const tempDir = mkdtempSync(join(tmpdir(), "forma-script-"));
  tempDirs.push(tempDir);
  return tempDir;
};

const parseEnvFile = (contents: string): Map<string, string> =>
  new Map(
    contents
      .trim()
      .split("\n")
      .map((line) => {
        const separatorIndex = line.indexOf("=");
        return [line.slice(0, separatorIndex), line.slice(separatorIndex + 1)];
      })
  );

const addFormaTraefikLabels = (composePath: string, hstsEnabled: "y" | "n", httpsSetup: "y" | "n"): void => {
  execFileSync(
    "bash",
    [
      "-lc",
      'source "$1"; add_forma_traefik_labels "$2" "example.com" "$3" "$4"',
      "bash",
      formaScriptPath,
      composePath,
      hstsEnabled,
      httpsSetup,
    ],
    { encoding: "utf8" }
  );
};

const writeDockerComposeTemplate = (): string => {
  const tempDir = createTempDir();
  const composePath = join(tempDir, "docker-compose.yml");

  writeFileSync(composePath, readFileSync(dockerComposeTemplatePath, "utf8"));

  return composePath;
};

const getDockerComposeProcessEnv = (overrides: NodeJS.ProcessEnv = {}): NodeJS.ProcessEnv => {
  const environment = { ...process.env };

  for (const key of dockerComposeOverrideKeys) {
    delete environment[key];
  }

  return { ...environment, ...overrides };
};

const runDockerCompose = (args: string[], environment: NodeJS.ProcessEnv = {}): string => {
  const result = spawnSync("docker", ["compose", ...args], {
    encoding: "utf8",
    env: getDockerComposeProcessEnv(environment),
    timeout: 20_000,
  });

  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || `Docker Compose exited with status ${result.status}`);
  }

  return result.stdout;
};

const withRequiredComposeEnv = (envContents: string): string => {
  const requiredValues = [
    ["AUTHZED_TOKEN", "test-authzed-token"],
    ["AUTHZED_DATABASE_PASSWORD", "test-authzed-database-password"],
  ];
  const missingValues = requiredValues
    .filter(([key]) => !new RegExp(`^${key}=`, "m").test(envContents))
    .map(([key, value]) => `${key}=${value}`)
    .join("\n");

  return [envContents.trimEnd(), missingValues].filter(Boolean).join("\n") + "\n";
};

const writeDockerComposeFixture = (envContents: string): { composePath: string; envPath: string } => {
  const composePath = writeDockerComposeTemplate();
  const envPath = join(dirname(composePath), ".env");

  writeFileSync(envPath, withRequiredComposeEnv(envContents));

  return { composePath, envPath };
};

const renderDockerCompose = (
  envContents: string,
  processEnvironment: NodeJS.ProcessEnv = {}
): RenderedDockerComposeConfig => {
  const { composePath, envPath } = writeDockerComposeFixture(envContents);

  return JSON.parse(
    runDockerCompose(
      [
        "--env-file",
        envPath,
        "--profile",
        "authzed-ops",
        "-f",
        composePath,
        "--project-directory",
        dirname(composePath),
        "config",
        "--format",
        "json",
      ],
      processEnvironment
    )
  ) as RenderedDockerComposeConfig;
};

const getRenderedServiceEnvironment = (
  config: RenderedDockerComposeConfig,
  serviceName: string
): Record<string, string> => {
  const environment = config.services[serviceName]?.environment;

  expect(environment).toBeDefined();
  return environment ?? {};
};

const getRenderedDockerComposeEnvironment = (
  composePath: string,
  envPath: string,
  processEnvironment: NodeJS.ProcessEnv = {}
): string =>
  runDockerCompose(
    [
      "--env-file",
      envPath,
      "-f",
      composePath,
      "--project-directory",
      dirname(composePath),
      "config",
      "--environment",
    ],
    processEnvironment
  );

const writeGeneratedEnvFile = (envPath: string, postgresPassword = ""): void => {
  execFileSync(
    "bash",
    [
      "-lc",
      'source "$1"; write_generated_env_file "$2" "$3"',
      "bash",
      formaScriptPath,
      envPath,
      postgresPassword,
    ],
    { encoding: "utf8" }
  );
};

const readExistingPostgresPassword = (
  envPath: string,
  composePath: string,
  processEnvironment: NodeJS.ProcessEnv = {}
): string => {
  const result = spawnSync(
    "bash",
    [
      "-c",
      'source "$1"; read_existing_postgres_password "$2" "$3"',
      "bash",
      formaScriptPath,
      envPath,
      composePath,
    ],
    {
      encoding: "utf8",
      env: getDockerComposeProcessEnv(processEnvironment),
      timeout: 20_000,
    }
  );

  if (result.error) {
    throw result.error;
  }
  if (result.status !== 0) {
    throw new Error(result.stderr.trim() || `Password discovery exited with status ${result.status}`);
  }

  return result.stdout;
};

const getDotenvValue = (envContents: string, key: string): string => {
  const value = envContents
    .split("\n")
    .find((line) => line.startsWith(`${key}=`))
    ?.slice(key.length + 1);

  expect(value).toBeDefined();
  return value ?? "";
};

const migrateLegacyValkeyImage = (composePath: string, validationResult: "success" | "failure"): string => {
  const validationLogPath = join(createTempDir(), "validation.log");

  execFileSync(
    "bash",
    [
      "-lc",
      `source "$1"
sudo() {
  printf '%s\\n' "$*" >> "$VALIDATION_LOG_PATH"
  [[ "$VALIDATION_RESULT" == "success" ]]
}
migrate_legacy_valkey_image "$2"`,
      "bash",
      formaScriptPath,
      composePath,
    ],
    {
      encoding: "utf8",
      env: {
        ...process.env,
        VALIDATION_LOG_PATH: validationLogPath,
        VALIDATION_RESULT: validationResult,
      },
    }
  );

  return validationLogPath;
};

const getServiceBlock = (composeContents: string, serviceName: string): string => {
  const lines = composeContents.split("\n");
  const startIndex = lines.findIndex((line) => line === `  ${serviceName}:`);

  expect(startIndex).toBeGreaterThanOrEqual(0);

  const endIndex = lines.findIndex((line, index) => index > startIndex && /^ {2}[A-Za-z0-9_-]+:/.test(line));

  return lines.slice(startIndex, endIndex === -1 ? undefined : endIndex).join("\n");
};

afterEach(() => {
  for (const tempDir of tempDirs.splice(0)) {
    rmSync(tempDir, { recursive: true, force: true });
  }
});

describe("Docker self-hosting credentials", () => {
  dockerComposeTest("rejects a missing PostgreSQL password", () => {
    expect(() => renderDockerCompose("")).toThrow(/POSTGRES_PASSWORD.*missing a value/);
  });

  dockerComposeTest("renders a URL-safe PostgreSQL password into every bundled database client", () => {
    const password = "url-safe-password";
    const config = renderDockerCompose(`POSTGRES_PASSWORD=${password}\n`);
    const formaDatabaseUrl = `postgresql://postgres:${password}@postgres:5432/forma?schema=public`;
    const postgresAdminUrl = `postgresql://postgres:${password}@postgres:5432/postgres?sslmode=disable`;

    expect(getRenderedServiceEnvironment(config, "postgres").POSTGRES_PASSWORD).toBe(password);
    expect(getRenderedServiceEnvironment(config, "authzed-db-bootstrap").POSTGRES_ADMIN_URL).toBe(
      postgresAdminUrl
    );
    expect(getRenderedServiceEnvironment(config, "forma-migrate").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "forma").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "authzed-ops").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "authzed-initialize").DATABASE_URL).toBe(formaDatabaseUrl);
  });

  dockerComposeTest("renders encoded connection URLs while retaining the raw PostgreSQL password", () => {
    const rawPassword = "legacy:p@ss/word?#";
    const encodedPassword = "legacy%3Ap%40ss%2Fword%3F%23";
    const config = renderDockerCompose(
      `POSTGRES_PASSWORD=${rawPassword}\nPOSTGRES_PASSWORD_URL_ENCODED=${encodedPassword}\n`
    );
    const formaDatabaseUrl = `postgresql://postgres:${encodedPassword}@postgres:5432/forma?schema=public`;
    const postgresAdminUrl = `postgresql://postgres:${encodedPassword}@postgres:5432/postgres?sslmode=disable`;

    expect(getRenderedServiceEnvironment(config, "postgres").POSTGRES_PASSWORD).toBe(rawPassword);
    expect(getRenderedServiceEnvironment(config, "authzed-db-bootstrap").POSTGRES_ADMIN_URL).toBe(
      postgresAdminUrl
    );
    expect(getRenderedServiceEnvironment(config, "forma-migrate").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "forma").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "authzed-ops").DATABASE_URL).toBe(formaDatabaseUrl);
    expect(getRenderedServiceEnvironment(config, "authzed-initialize").DATABASE_URL).toBe(formaDatabaseUrl);
  });

  test("writes generated credentials to a private local environment file", () => {
    const tempDir = createTempDir();
    const envPath = join(tempDir, ".env");
    const secondEnvPath = join(tempDir, ".env.second");

    writeGeneratedEnvFile(envPath);
    writeGeneratedEnvFile(secondEnvPath);

    const envContents = readFileSync(envPath, "utf8");
    const secondEnvContents = readFileSync(secondEnvPath, "utf8");
    const serializedPostgresPassword = getDotenvValue(envContents, "POSTGRES_PASSWORD");
    const serializedSecondPostgresPassword = getDotenvValue(secondEnvContents, "POSTGRES_PASSWORD");
    const postgresPassword = serializedPostgresPassword.slice(1, -1);
    const secondPostgresPassword = serializedSecondPostgresPassword.slice(1, -1);

    expect(serializedPostgresPassword).toMatch(/^"[a-f0-9]{64}"$/);
    expect(serializedSecondPostgresPassword).toMatch(/^"[a-f0-9]{64}"$/);
    expect(envContents).toContain(`POSTGRES_PASSWORD_URL_ENCODED=${postgresPassword}`);
    expect(envContents).toMatch(/^AUTHZED_TOKEN=[a-f0-9]{64}$/m);
    expect(envContents).toMatch(/^AUTHZED_DATABASE_PASSWORD=[a-f0-9]{64}$/m);
    expect(envContents).toContain(`
`);
    expect(secondPostgresPassword).not.toBe(postgresPassword);
    expect(statSync(envPath).mode & 0o777).toBe(0o600);
    expect(statSync(secondEnvPath).mode & 0o777).toBe(0o600);
  });

  test("leaves the existing password empty when no deployment artifacts exist", () => {
    const tempDir = createTempDir();

    expect(readExistingPostgresPassword(join(tempDir, ".env"), join(tempDir, "docker-compose.yml"))).toBe("");
  });

  test("reuses sudo Docker Compose access selected by installer preflight", () => {
    const tempDir = createTempDir();
    const binPath = join(tempDir, "bin");
    const dockerPath = join(binPath, "docker");
    const sudoPath = join(binPath, "sudo");
    const callLogPath = join(tempDir, "docker-calls.log");
    const envPath = join(tempDir, ".env");
    const composePath = join(tempDir, "docker-compose.yml");

    mkdirSync(binPath);
    writeFileSync(envPath, "POSTGRES_PASSWORD=legacy-password\n");
    writeFileSync(composePath, "services:\n  postgres:\n    image: pgvector/pgvector:pg18\n");
    writeFileSync(
      dockerPath,
      `#!/bin/sh
printf 'docker %s\\n' "$*" >> "$FORMA_DOCKER_CALL_LOG"
exit 1
`
    );
    writeFileSync(
      sudoPath,
      `#!/bin/sh
printf 'sudo %s\\n' "$*" >> "$FORMA_DOCKER_CALL_LOG"
if [ "$1 $2" = "docker info" ]; then
  exit 0
fi
if [ "$1 $2 $3" = "docker compose version" ]; then
  exit 0
fi
if [ "$1 $2" = "docker compose" ]; then
  printf '%s' '{"services":{"postgres":{"environment":{"POSTGRES_PASSWORD":"legacy-password"}}}}'
  exit 0
fi
exit 1
`
    );
    chmodSync(dockerPath, 0o755);
    chmodSync(sudoPath, 0o755);

    const recoveredPassword = execFileSync(
      "bash",
      [
        "-c",
        'source "$1"; configure_forma_docker_command; run_forma_docker_compose version >/dev/null; read_existing_postgres_password "$2" "$3"',
        "bash",
        formaScriptPath,
        envPath,
        composePath,
      ],
      {
        encoding: "utf8",
        env: getDockerComposeProcessEnv({
          FORMA_DOCKER_CALL_LOG: callLogPath,
          PATH: `${binPath}:${process.env.PATH ?? ""}`,
        }),
      }
    );
    const callLog = readFileSync(callLogPath, "utf8");

    expect(recoveredPassword).toBe("legacy-password");
    expect(callLog).toContain("docker info");
    expect(callLog).toContain("sudo docker info");
    expect(callLog).toContain("sudo docker compose version");
    expect(callLog).toContain("sudo docker compose --env-file");
  });

  dockerComposeTest("preserves and URL-encodes the password from an existing one-click installation", () => {
    const tempDir = createTempDir();
    const envPath = join(tempDir, ".env");
    const composePath = join(tempDir, "docker-compose.yml");

    writeFileSync(
      composePath,
      `services:
  postgres:
    image: pgvector/pgvector:pg18
    environment:
      - POSTGRES_PASSWORD=legacy:p@ss/word?#
`
    );
    writeFileSync(envPath, "CUSTOM_SETTING=preserved\n");

    const existingPassword = readExistingPostgresPassword(envPath, composePath);
    writeGeneratedEnvFile(envPath, existingPassword);

    const envContents = readFileSync(envPath, "utf8");

    expect(existingPassword).toBe("legacy:p@ss/word?#");
    expect(envContents).toContain('POSTGRES_PASSWORD="legacy:p@ss/word?#"');
    expect(envContents).toContain("POSTGRES_PASSWORD_URL_ENCODED=legacy%3Ap%40ss%2Fword%3F%23");
    expect(envContents).toContain("CUSTOM_SETTING=preserved");
  });

  dockerComposeTest("uses Docker Compose semantics to resolve existing dotenv passwords", () => {
    const cases = [
      {
        envContents: "POSTGRES_PASSWORD: 'legacy$$value'\n",
        expectedPassword: "legacy$$value",
      },
      {
        envContents: 'POSTGRES_PASSWORD="legacy\\\\path"\n',
        expectedPassword: "legacy\\path",
      },
      {
        envContents: "POSTGRES_PASSWORD=legacy-password # operator note\n",
        expectedPassword: "legacy-password",
      },
      {
        envContents: "PASSWORD_SUFFIX=word\nPOSTGRES_PASSWORD=prefix-${PASSWORD_SUFFIX}\n",
        expectedPassword: "prefix-word",
      },
    ];

    for (const { envContents, expectedPassword } of cases) {
      const composePath = writeDockerComposeTemplate();
      const envPath = join(dirname(composePath), ".env");

      writeFileSync(envPath, withRequiredComposeEnv(envContents));

      expect(readExistingPostgresPassword(envPath, composePath)).toBe(expectedPassword);
    }
  });

  dockerComposeTest("reads legacy Compose passwords through the rendered configuration", () => {
    const tempDir = createTempDir();
    const envPath = join(tempDir, ".env");
    const composePath = join(tempDir, "docker-compose.yml");
    const missingComposePath = join(tempDir, "missing-compose.yml");
    const writeLegacyComposePassword = (passwordLine: string): void => {
      writeFileSync(
        composePath,
        `services:
  postgres:
    image: pgvector/pgvector:pg18
    environment:
      ${passwordLine}
`
      );
    };

    writeFileSync(envPath, "POSTGRES_PASSWORD='legacy-password'\n");
    expect(() => readExistingPostgresPassword(envPath, missingComposePath)).toThrow(
      /Could not safely resolve/
    );
    rmSync(envPath);

    writeLegacyComposePassword("- POSTGRES_PASSWORD=legacy:p@ss/word?#!&'()*;[]");
    expect(readExistingPostgresPassword(envPath, composePath)).toBe("legacy:p@ss/word?#!&'()*;[]");

    const quotedPassword = `legacy:p@ss/word?#!&'()*;[]\\path"quoted" `;
    writeLegacyComposePassword(`POSTGRES_PASSWORD: ${JSON.stringify(quotedPassword)}`);
    expect(readExistingPostgresPassword(envPath, composePath)).toBe(quotedPassword);

    writeLegacyComposePassword("- POSTGRES_PASSWORD=legacy$$PASSWORD_SENTINEL");
    expect(readExistingPostgresPassword(envPath, composePath)).toBe("legacy$PASSWORD_SENTINEL");

    writeLegacyComposePassword("- POSTGRES_PASSWORD=$FORMA_UNSET_PASSWORD_SENTINEL");
    const unresolvedConfig = JSON.parse(
      runDockerCompose([
        "-f",
        composePath,
        "--project-directory",
        dirname(composePath),
        "config",
        "--format",
        "json",
      ])
    ) as RenderedDockerComposeConfig;

    expect(getRenderedServiceEnvironment(unresolvedConfig, "postgres").POSTGRES_PASSWORD).toBe("");
    expect(() => readExistingPostgresPassword(envPath, composePath)).toThrow(/Could not safely resolve/);

    writeLegacyComposePassword("- POSTGRES_PASSWORD=legacy-password # operator note");
    expect(readExistingPostgresPassword(envPath, composePath)).toBe("legacy-password");

    writeLegacyComposePassword(`POSTGRES_PASSWORD: ${JSON.stringify("legacy\npassword")}`);
    expect(() => readExistingPostgresPassword(envPath, composePath)).toThrow(/Could not safely resolve/);
  });

  dockerComposeTest("preserves unrelated environment entries and literal dollar signs across reruns", () => {
    const composePath = writeDockerComposeTemplate();
    const envPath = join(dirname(composePath), ".env");
    const password = "legacy$PASSWORD_SENTINEL";

    writeFileSync(
      envPath,
      withRequiredComposeEnv(`# Operator-managed settings
PUBLIC_URL=https://surveys.example.com
CUSTOM_SECRET=keep-me
POSTGRES_PASSWORD='legacy$PASSWORD_SENTINEL'
`)
    );

    const quotedExistingPassword = readExistingPostgresPassword(envPath, composePath, {
      PASSWORD_SENTINEL: "rewritten",
    });
    writeGeneratedEnvFile(envPath, quotedExistingPassword);

    const firstEnvContents = readFileSync(envPath, "utf8");
    const renderedEnvironment = getRenderedDockerComposeEnvironment(composePath, envPath, {
      PASSWORD_SENTINEL: "rewritten",
    });
    const renderedConfig = renderDockerCompose(firstEnvContents, {
      PASSWORD_SENTINEL: "rewritten",
    });
    const renderedPostgresPassword = getRenderedServiceEnvironment(
      renderedConfig,
      "postgres"
    ).POSTGRES_PASSWORD.replaceAll("$$", "$");
    const existingPassword = readExistingPostgresPassword(envPath, composePath, {
      PASSWORD_SENTINEL: "rewritten",
    });
    writeGeneratedEnvFile(envPath, existingPassword);

    const rerunEnvContents = readFileSync(envPath, "utf8");

    expect(existingPassword).toBe(password);
    expect(rerunEnvContents).toContain("# Operator-managed settings");
    expect(rerunEnvContents).toContain("PUBLIC_URL=https://surveys.example.com");
    expect(rerunEnvContents).toContain("CUSTOM_SECRET=keep-me");
    expect(rerunEnvContents.match(/^POSTGRES_PASSWORD=/gm)).toHaveLength(1);
    expect(rerunEnvContents.match(/^AUTHZED_TOKEN=/gm)).toHaveLength(1);
    expect(rerunEnvContents.match(/^AUTHZED_DATABASE_PASSWORD=/gm)).toHaveLength(1);
    expect(statSync(envPath).mode & 0o777).toBe(0o600);
  });

  dockerComposeTest("serializes preserved passwords without dotenv reinterpretation", () => {
    const composePath = writeDockerComposeTemplate();
    const envPath = join(dirname(composePath), ".env");
    const password = 'my secret #1 a\\\'b "quoted" $PASSWORD_SENTINEL ';
    const encodedPassword = "my%20secret%20%231%20a%5C%27b%20%22quoted%22%20%24PASSWORD_SENTINEL%20";

    writeGeneratedEnvFile(envPath, password);

    const firstEnvContents = readFileSync(envPath, "utf8");
    const firstReadPassword = readExistingPostgresPassword(envPath, composePath, {
      PASSWORD_SENTINEL: "rewritten",
    });

    expect(firstReadPassword).toBe(password);
    expect(getDotenvValue(firstEnvContents, "POSTGRES_PASSWORD")).toMatch(/^".*"$/);
    expect(getDotenvValue(firstEnvContents, "POSTGRES_PASSWORD_URL_ENCODED")).toBe(encodedPassword);

    writeGeneratedEnvFile(envPath, firstReadPassword);

    expect(
      readExistingPostgresPassword(envPath, composePath, {
        PASSWORD_SENTINEL: "rewritten",
      })
    ).toBe(password);
  });
});

describe("docker/forma.sh AuthZed setup", () => {
  test("writes AuthZed secrets without printing them", () => {
    const envPath = join(createTempDir(), ".env");
    const authzedToken = "authzed-token-value";
    const authzedDatabasePassword = "authzed-database-password";

    const output = execFileSync(
      "bash",
      [
        "-lc",
        'source "$1"; write_base_env_file "$2" "$3" "$4"',
        "bash",
        formaScriptPath,
        envPath,
        authzedToken,
        authzedDatabasePassword,
      ],
      { encoding: "utf8" }
    );

    const env = parseEnvFile(readFileSync(envPath, "utf8"));

    expect(output).toBe("");
    expect(output).not.toContain(authzedToken);
    expect(output).not.toContain(authzedDatabasePassword);
    expect(env.get("AUTHZED_TOKEN")).toBe(authzedToken);
    expect(env.get("AUTHZED_DATABASE_PASSWORD")).toBe(authzedDatabasePassword);
    expect(env.get("AUTHZED_ENABLED")).toBe("true");
    expect(env.get("AUTHZED_CONSISTENCY")).toBe("fully_consistent");
    expect(env.get("FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED")).toBe("true");
    expect(statSync(envPath).mode & 0o777).toBe(0o600);
  });

  test("blocks customized updates until the AuthZed v6 contract is present and acknowledged", () => {
    const script = readFileSync(formaScriptPath, "utf8");
    const updateFunction = script.slice(script.indexOf("update_forma()"), script.indexOf("restart_forma()"));

    expect(updateFunction).toContain(
      "This installation does not yet contain the AuthZed v6 Compose services"
    );
    expect(updateFunction).toContain("FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED=true");
    expect(updateFunction).toContain("authzed-ops upgrade prepare");
    expect(updateFunction).toContain("authzed-ops upgrade check");
    expect(updateFunction.indexOf("upgrade check")).toBeLessThan(updateFunction.indexOf("compose down"));
  });

  test("runs the upgrade gates before stopping an existing installation", () => {
    const tempDir = createTempDir();
    const installationDir = join(tempDir, "forma");
    const binDir = join(tempDir, "bin");
    const commandLog = join(tempDir, "commands.log");
    mkdirSync(installationDir, { recursive: true });
    mkdirSync(binDir, { recursive: true });
    writeFileSync(join(installationDir, "docker-compose.yml"), "services:\n  authzed-ops:\n  spicedb:\n");
    writeFileSync(join(installationDir, ".env"), "FORMA_AUTHZED_V6_MIGRATION_ACKNOWLEDGED=true\n");
    writeFileSync(join(binDir, "sudo"), '#!/bin/sh\nprintf "%s\\n" "$*" >> "$COMMAND_LOG"\n', {
      mode: 0o700,
    });

    const result = spawnSync("bash", ["-c", 'source "$1"; update_forma', "bash", formaScriptPath], {
      cwd: tempDir,
      encoding: "utf8",
      env: { ...process.env, COMMAND_LOG: commandLog, PATH: `${binDir}:${process.env.PATH ?? ""}` },
    });

    expect(result.status).toBe(0);
    const commands = readFileSync(commandLog, "utf8").trim().split("\n");
    expect(commands).toEqual([
      "docker compose pull",
      "docker compose run --rm forma-migrate",
      "docker compose --profile authzed-ops run --rm authzed-ops upgrade prepare",
      "docker compose --profile authzed-ops run --rm authzed-ops upgrade check",
      "docker compose down",
      "docker compose up -d",
    ]);
  });

  test("waits for the source migration before preparing a fresh AuthZed graph", () => {
    const script = readFileSync(formaScriptPath, "utf8");
    const setupStart = script.indexOf(
      "docker compose up -d postgres authzed-db-bootstrap spicedb-migrate spicedb forma-migrate"
    );
    const migrationWait = script.indexOf("docker compose wait forma-migrate", setupStart);
    const upgradePrepare = script.indexOf("authzed-ops upgrade prepare", setupStart);

    expect(setupStart).toBeGreaterThanOrEqual(0);
    expect(migrationWait).toBeGreaterThan(setupStart);
    expect(upgradePrepare).toBeGreaterThan(migrationWait);
  });

  test("pins and verifies the downloaded bootstrap helper before making it executable", () => {
    const script = readFileSync(formaScriptPath, "utf8");
    const downloadStart = script.indexOf(
      'authzed_bootstrap_commit="10d5ad908491a8a818aef3c6ada91fa4fdc30b03"'
    );
    const checksumStart = script.indexOf("sha256sum --check --status -", downloadStart);
    const chmodStart = script.indexOf("chmod 700 authzed-postgres-bootstrap.sh", checksumStart);

    expect(downloadStart).toBeGreaterThanOrEqual(0);
    expect(script.slice(downloadStart, checksumStart)).not.toContain(
      "forma/stable/docker/authzed-postgres-bootstrap.sh"
    );
    expect(script.slice(downloadStart, checksumStart)).toContain(
      "70975701cdf0dcffef5d3573a7514360e87428bb07cc4bfb4dbf47ae0c2e93a5"
    );
    expect(checksumStart).toBeGreaterThan(downloadStart);
    expect(chmodStart).toBeGreaterThan(checksumStart);
  });
});

describe("docker/docker-compose.yml Redis/Valkey exposure (ENG-2184)", () => {
  // The bundled Valkey is Better Auth's session/token store (secondaryStorage). Publishing it to
  // the host binds 0.0.0.0:6379 with no password, exposing every live session token — and Docker's
  // port rule bypasses host firewalls like ufw. The app reaches it over the internal compose
  // network (REDIS_URL=redis://redis:6379), so no host publish is needed. Assert on the *resolved*
  // compose model — js-yaml expands anchors and merge keys — so a port smuggled in via an alias or
  // a `<<` merge cannot slip past a raw-text check.
  test("the redis service publishes no host port", () => {
    const doc = load(readFileSync(dockerComposeTemplatePath, "utf8")) as {
      services?: Record<string, { ports?: unknown }>;
    };
    const redis = doc.services?.redis;

    expect(redis, "the redis service is missing from docker-compose.yml").toBeTypeOf("object");
    expect(
      redis?.ports ?? [],
      "the redis (Valkey) service must not publish any host port — it is reachable only on the internal compose network (ENG-2184)"
    ).toEqual([]);
  });
});

describe("docker/forma.sh Valkey image migration", () => {
  test("updates the known amd64-only pin and keeps a backup", () => {
    const composePath = writeDockerComposeTemplate();
    const originalCompose = readFileSync(composePath, "utf8");

    expect(originalCompose).toContain(multiArchValkeyImage);
    writeFileSync(composePath, originalCompose.replace(multiArchValkeyImage, legacyValkeyImage));
    chmodSync(composePath, 0o600);

    const validationLogPath = migrateLegacyValkeyImage(composePath, "success");

    expect(readFileSync(composePath, "utf8")).toBe(originalCompose);
    expect(readFileSync(`${composePath}.before-valkey-8.1.9`, "utf8")).toContain(legacyValkeyImage);
    expect(statSync(composePath).mode & 0o777).toBe(0o600);
    expect(statSync(`${composePath}.before-valkey-8.1.9`).mode & 0o777).toBe(0o600);
    expect(readFileSync(validationLogPath, "utf8")).toBe(`docker compose -f ${composePath} config\n`);
  });

  test("updates the live redis service when Compose uses four-space indentation", () => {
    const tempDir = createTempDir();
    const composePath = join(tempDir, "docker-compose.yml");
    writeFileSync(
      composePath,
      `services:
    redis:
        image: ${legacyValkeyImage}
        volumes:
            - redis:/data

volumes:
    redis:
`
    );

    const validationLogPath = migrateLegacyValkeyImage(composePath, "success");

    expect(readFileSync(composePath, "utf8")).toContain(multiArchValkeyImage);
    expect(readFileSync(`${composePath}.before-valkey-8.1.9`, "utf8")).toContain(legacyValkeyImage);
    expect(readFileSync(validationLogPath, "utf8")).toBe(`docker compose -f ${composePath} config\n`);
  });

  test.each([multiArchValkeyImage, "example.com/custom-valkey@sha256:custom"])(
    "leaves the %s reference untouched",
    (image) => {
      const composePath = writeDockerComposeTemplate();
      const originalCompose = readFileSync(composePath, "utf8").replace(multiArchValkeyImage, image);
      writeFileSync(composePath, originalCompose);

      migrateLegacyValkeyImage(composePath, "failure");

      expect(readFileSync(composePath, "utf8")).toBe(originalCompose);
      expect(existsSync(`${composePath}.before-valkey-8.1.9`)).toBe(false);
    }
  );

  test("restores the original Compose file when validation fails", () => {
    const composePath = writeDockerComposeTemplate();
    const legacyCompose = readFileSync(composePath, "utf8").replace(multiArchValkeyImage, legacyValkeyImage);
    writeFileSync(composePath, legacyCompose);

    expect(() => migrateLegacyValkeyImage(composePath, "failure")).toThrow();

    expect(readFileSync(composePath, "utf8")).toBe(legacyCompose);
    expect(readFileSync(`${composePath}.before-valkey-8.1.9`, "utf8")).toBe(legacyCompose);
    expect(existsSync(`${composePath}.tmp`)).toBe(false);
  });
});

describe("docker/forma.sh Traefik label injection", () => {
  test("adds HTTPS Traefik labels to the forma service only", () => {
    const composePath = writeDockerComposeTemplate();

    addFormaTraefikLabels(composePath, "y", "y");

    const composeContents = readFileSync(composePath, "utf8");
    const formaMigrateBlock = getServiceBlock(composeContents, "forma-migrate");
    const formaBlock = getServiceBlock(composeContents, "forma");
    const authzedBootstrapBlock = getServiceBlock(composeContents, "authzed-db-bootstrap");
    const authzedOpsBlock = getServiceBlock(composeContents, "authzed-ops");
    const authzedInitializeBlock = getServiceBlock(composeContents, "authzed-initialize");
    const spicedbBlock = getServiceBlock(composeContents, "spicedb");

    expect(formaMigrateBlock).not.toContain("    labels:");
    expect(formaMigrateBlock).not.toContain("traefik.enable=true");
    expect(authzedBootstrapBlock).toContain("authzed-postgres-bootstrap.sh");
    expect(authzedBootstrapBlock).not.toContain("traefik.enable=true");
    expect(spicedbBlock).toContain("authzed/spicedb:v1.52.0");
    expect(spicedbBlock).not.toContain("traefik.enable=true");
    expect(authzedOpsBlock).toContain('profiles: ["authzed-ops"]');
    expect(authzedOpsBlock).not.toContain("traefik.enable=true");
    expect(authzedInitializeBlock).toContain('command: ["upgrade", "prepare"]');
    expect(authzedInitializeBlock).not.toContain("traefik.enable=true");
    expect(formaBlock).toContain("    labels:");
    expect(formaBlock.indexOf("    labels:")).toBeLessThan(formaBlock.indexOf("    environment:"));
    expect(formaBlock).toContain("traefik.http.routers.forma.rule=Host(`example.com`)");
    expect(formaBlock).toContain("traefik.http.routers.forma.entrypoints=websecure");
    expect(formaBlock).toContain("traefik.http.routers.forma.tls.certresolver=default");
    expect(formaBlock).toContain("traefik.http.services.forma.loadbalancer.server.port=3000");
    expect(formaBlock).toContain("traefik.http.middlewares.hstsHeader.headers.stsSeconds=31536000");
    expect(formaBlock).not.toContain("traefik.http.routers.forma_http.entrypoints=web");
  });

  test("omits ACME certresolver labels when HTTPS setup is disabled", () => {
    const composePath = writeDockerComposeTemplate();

    addFormaTraefikLabels(composePath, "y", "n");

    const composeContents = readFileSync(composePath, "utf8");
    const formaBlock = getServiceBlock(composeContents, "forma");

    expect(formaBlock).toContain("traefik.http.routers.forma.entrypoints=websecure");
    expect(formaBlock).toContain("traefik.http.routers.forma.tls=true");
    expect(formaBlock).toContain("traefik.http.middlewares.hstsHeader.headers.stsSeconds=31536000");
    expect(formaBlock).not.toContain("tls.certresolver=default");
  });

  test("adds HTTP fallback labels when HSTS is disabled", () => {
    const composePath = writeDockerComposeTemplate();

    addFormaTraefikLabels(composePath, "n", "n");

    const composeContents = readFileSync(composePath, "utf8");
    const formaMigrateBlock = getServiceBlock(composeContents, "forma-migrate");
    const formaBlock = getServiceBlock(composeContents, "forma");

    expect(formaMigrateBlock).not.toContain("    labels:");
    expect(formaBlock).toContain("    labels:");
    expect(formaBlock).toContain("traefik.http.routers.forma_http.entrypoints=web");
    expect(formaBlock).toContain("traefik.http.routers.forma_http.rule=Host(`example.com`)");
    expect(formaBlock).not.toContain("tls.certresolver=default");
    expect(formaBlock).not.toContain("traefik.http.middlewares.hstsHeader.headers.stsSeconds=31536000");
  });

  test("fails when the forma service insertion point is missing", () => {
    const tempDir = createTempDir();
    const composePath = join(tempDir, "docker-compose.yml");

    writeFileSync(
      composePath,
      `services:
  forma:
    image: ghcr.io/yldm-tech/forma:latest
`
    );

    expect(() => {
      addFormaTraefikLabels(composePath, "y", "y");
    }).toThrow();
    expect(existsSync(`${composePath}.tmp`)).toBe(false);
  });
});
