import { lazy, Suspense, useState } from 'react'

// Reports is a separate chunk, so the fixture exercises Vite's preload code.
const Reports = lazy(() => import('./Reports.jsx'))

const products = ['Kettle', 'Teapot', 'Mug', 'Strainer', 'Tray']

export default function App() {
  const [showReports, setShowReports] = useState(false)
  return (
    <main>
      <header>
        <img src="/logo.svg" alt="Fixture shop logo" width="32" height="32" />
        <h1>Fixture shop</h1>
      </header>
      <ul id="products">
        {products.map((p) => (
          <li key={p}>{p}</li>
        ))}
      </ul>
      <button id="show-reports" type="button" onClick={() => setShowReports(true)}>
        Show reports
      </button>
      {showReports && (
        <Suspense fallback={<p>Loading</p>}>
          <Reports />
        </Suspense>
      )}
    </main>
  )
}
