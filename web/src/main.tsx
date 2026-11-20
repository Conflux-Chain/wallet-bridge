import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { App } from './App';
import { initialize } from './api/browser';
import './style.css';

const initial = initialize();
// A pasted link can be a same-document navigation; consume it on a fresh boot.
window.addEventListener('hashchange', () => { location.reload(); });

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App initial={initial} />
  </StrictMode>,
);
