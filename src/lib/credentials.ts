import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";
import { keychainGet, keychainSet, keychainDelete } from "./keychain.ts";

export type Credential = {
  username?: string;
  password?: string;
  writePermission: boolean;
};

type CredentialListEntry = {
  name: string;
  username?: string;
  password?: string;
  writePermission: boolean;
};

type StoredCredential = {
  username?: string;
  password?: string;
  writePermission: boolean;
  keychainManaged?: boolean;
};

type CredentialStore = Record<string, StoredCredential>;

const KEYCHAIN_PLACEHOLDER = "__KEYCHAIN__";

const getConfigDir = (): string => {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  if (xdg) {
    return join(xdg, "agent-sql");
  }
  return join(homedir(), ".config", "agent-sql");
};

const ensureConfigDir = (): string => {
  const dir = getConfigDir();
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
  return dir;
};

const credentialsFilePath = (): string => join(getConfigDir(), "credentials.json");

const readCredentialFile = (): CredentialStore => {
  const p = credentialsFilePath();
  if (!existsSync(p)) {
    return {};
  }
  try {
    return JSON.parse(readFileSync(p, "utf8")) as CredentialStore;
  } catch {
    return {};
  }
};

const writeCredentialFile = (store: CredentialStore): void => {
  const dir = ensureConfigDir();
  writeFileSync(join(dir, "credentials.json"), `${JSON.stringify(store, null, 2)}\n`, "utf8");
};

export const storeCredential = (
  name: string,
  credential: Credential,
): { storage: "keychain" | "file" } => {
  const keychainValue = JSON.stringify(credential);
  const stored = keychainSet({ account: name, value: keychainValue });

  const fileStore = readCredentialFile();
  if (stored) {
    // Keychain holds the real data; file is an index with placeholders
    const entry: StoredCredential = {
      writePermission: credential.writePermission,
      keychainManaged: true,
    };
    if (credential.username !== undefined) {
      entry.username = KEYCHAIN_PLACEHOLDER;
    }
    if (credential.password !== undefined) {
      entry.password = KEYCHAIN_PLACEHOLDER;
    }
    fileStore[name] = entry;
    writeCredentialFile(fileStore);
    return { storage: "keychain" };
  }

  // File-only storage
  fileStore[name] = { ...credential };
  writeCredentialFile(fileStore);
  return { storage: "file" };
};

export const getCredential = (name: string): Credential | null => {
  const fileStore = readCredentialFile();
  const entry = fileStore[name];
  if (!entry) {
    return null;
  }

  if (entry.keychainManaged) {
    const raw = keychainGet(name);
    if (!raw) {
      return null;
    }
    try {
      return JSON.parse(raw) as Credential;
    } catch {
      return null;
    }
  }

  const result: Credential = { writePermission: entry.writePermission };
  if (entry.username !== undefined) {
    result.username = entry.username;
  }
  if (entry.password !== undefined) {
    result.password = entry.password;
  }
  return result;
};

export const removeCredential = (name: string): boolean => {
  const fileStore = readCredentialFile();
  const entry = fileStore[name];
  if (!entry) {
    return false;
  }

  if (entry.keychainManaged) {
    keychainDelete(name);
  }
  delete fileStore[name];
  writeCredentialFile(fileStore);
  return true;
};

export const listCredentials = (): CredentialListEntry[] => {
  const fileStore = readCredentialFile();
  return Object.entries(fileStore).map(([name, entry]) => {
    const result: CredentialListEntry = { name, writePermission: entry.writePermission };
    if (entry.keychainManaged) {
      // Resolve from Keychain for display
      const raw = keychainGet(name);
      if (raw) {
        try {
          const cred = JSON.parse(raw) as Credential;
          if (cred.username !== undefined) {
            result.username = cred.username;
          }
          if (cred.password !== undefined) {
            result.password = "********";
          }
        } catch {
          // fall through with index-only data
        }
      }
    } else {
      if (entry.username !== undefined) {
        result.username = entry.username;
      }
      if (entry.password !== undefined) {
        result.password = "********";
      }
    }
    return result;
  });
};

export const getCredentialNames = (): string[] => Object.keys(readCredentialFile());
