import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { GlobalErrors } from './GlobalErrors.tsx'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <GlobalErrors>
      <App />
    </GlobalErrors>
  </StrictMode>,
)
