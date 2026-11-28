import type {
  AdministratorSession,
  LoginCredentials,
  RackOSAPIClient,
} from "@rackos/api-client";

export type AuthState =
  | {
      status: "checking";
    }
  | {
      status: "anonymous";
    }
  | {
      status: "authenticating";
    }
  | {
      status: "authenticated";
      session: AdministratorSession;
    }
  | {
      status: "signing-out";
    }
  | {
      status: "unavailable";
    };

export type AuthClient = Pick<
  RackOSAPIClient,
  | "login"
  | "logout"
  | "getSession"
  | "recordActivity"
>;

export interface AuthContextValue {
  state: AuthState;

  sessionGeneration: number;

  restoreSession(): Promise<void>;

  login(
    credentials: LoginCredentials,
  ): Promise<void>;

  logout(): Promise<void>;

  recordActivity(): Promise<void>;

  captureManagementUnauthorized():
    (() => void) | undefined;
}