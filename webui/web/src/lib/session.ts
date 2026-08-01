import { sessionApiBaseKey, sessionTokenKey } from './project';

export type SessionState = {
  apiBase: string;
  token: string;
};

export function loadSession(storage: Storage, origin = window.location.origin): SessionState {
  return {
    apiBase: storage.getItem(sessionApiBaseKey) || origin,
    token: storage.getItem(sessionTokenKey) || ''
  };
}

// takeLaunchToken consumes the `#token=…` fragment `inferencerig web` prints,
// so opening the printed URL is the whole login. The token rides in the
// fragment rather than the query because a fragment is never sent to the
// server and so cannot appear in an access log; it is stripped from the
// address bar immediately, since a URL that survives in history or a shared
// screenshot is a leaked credential.
export function takeLaunchToken(location: Location, history: History): string {
  const token = new URLSearchParams(location.hash.replace(/^#/, '')).get('token')?.trim();
  if (!token) return '';
  history.replaceState(null, '', location.pathname + location.search);
  return token;
}

// isLoopbackHost reports whether this page was loaded from the local machine.
// It is what separates "insecure mode, single user, fine" from "insecure mode,
// reachable over the network, alarming".
export function isLoopbackHost(hostname: string): boolean {
  return hostname === 'localhost' || hostname === '127.0.0.1' || hostname === '[::1]' || hostname === '::1';
}

export function saveApiBase(storage: Storage, apiBase: string) {
  storage.setItem(sessionApiBaseKey, apiBase.trim());
}

export function saveToken(storage: Storage, token: string) {
  storage.setItem(sessionTokenKey, token);
}
