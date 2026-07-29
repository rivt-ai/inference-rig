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

export function saveApiBase(storage: Storage, apiBase: string) {
  storage.setItem(sessionApiBaseKey, apiBase.trim());
}

export function saveToken(storage: Storage, token: string) {
  storage.setItem(sessionTokenKey, token);
}
