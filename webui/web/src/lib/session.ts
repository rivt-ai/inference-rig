import { projectName, sessionApiBaseKey, sessionTokenKey } from './project';

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

// The insecure-exposure banner is dismissible, but the dismissal is recorded
// per host rather than globally. The warning is about *this* address being
// reachable without a credential, so acknowledging it on one host says nothing
// about the next: a laptop that moves networks, or a gateway later bound
// somewhere new, has to raise it again. A single global flag would silence the
// warning permanently on the first click, which is how a security notice
// becomes decorative.
function insecureDismissKey(host: string) {
  return `${projectName}.insecureBannerDismissed.${host}`;
}

export function loadInsecureDismissed(storage: Storage, host = window.location.host): boolean {
  return storage.getItem(insecureDismissKey(host)) === 'true';
}

export function saveInsecureDismissed(storage: Storage, host = window.location.host) {
  storage.setItem(insecureDismissKey(host), 'true');
}
