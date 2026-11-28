export interface LoginCredentials {
  username: string;
  password: string;
}

/**
 * Public administrator session metadata.
 *
 * Authentication tokens remain exclusively in
 * secure HttpOnly browser cookies.
 */
export interface AdministratorSession {
  administrator_id: string;

  created_at: string;
  last_activity_at: string;

  expires_at: string;
  idle_expires_at: string;
}