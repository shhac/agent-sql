import { platform } from "node:os";
import { execFileSync } from "node:child_process";

const IS_MACOS = platform() === "darwin";

export const KEYCHAIN_SERVICE = "app.paulie.agent-sql";

export const keychainGet = (account: string): string | null => {
  if (!IS_MACOS) {
    return null;
  }
  try {
    const result = execFileSync(
      "security",
      ["find-generic-password", "-s", KEYCHAIN_SERVICE, "-a", account, "-w"],
      { encoding: "utf8", stdio: ["pipe", "pipe", "ignore"] },
    );
    return result.trim() || null;
  } catch {
    return null;
  }
};

export const keychainSet = (opts: { account: string; value: string }): boolean => {
  if (!IS_MACOS) {
    return false;
  }
  const { account, value } = opts;
  try {
    try {
      execFileSync("security", ["delete-generic-password", "-s", KEYCHAIN_SERVICE, "-a", account], {
        stdio: ["pipe", "pipe", "ignore"],
      });
    } catch {
      /* entry may not exist */
    }
    execFileSync(
      "security",
      ["add-generic-password", "-s", KEYCHAIN_SERVICE, "-a", account, "-w", value],
      { stdio: ["pipe", "pipe", "ignore"] },
    );
    return true;
  } catch {
    return false;
  }
};

export const keychainDelete = (account: string): boolean => {
  if (!IS_MACOS) {
    return false;
  }
  try {
    execFileSync("security", ["delete-generic-password", "-s", KEYCHAIN_SERVICE, "-a", account], {
      stdio: ["pipe", "pipe", "ignore"],
    });
    return true;
  } catch {
    return false;
  }
};

export const keychainDeleteAll = (): void => {
  if (!IS_MACOS) {
    return;
  }
  for (;;) {
    try {
      execFileSync("security", ["delete-generic-password", "-s", KEYCHAIN_SERVICE], {
        stdio: ["pipe", "pipe", "ignore"],
      });
    } catch {
      break;
    }
  }
};
