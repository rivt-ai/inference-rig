import './styles/app.css';
import App from './App.svelte';
import { mount } from 'svelte';
import { applyPrimaryColors, loadPrimaryColors } from './lib/theme';

applyPrimaryColors(loadPrimaryColors(localStorage));

mount(App, {
  target: document.getElementById('app') as HTMLElement
});
