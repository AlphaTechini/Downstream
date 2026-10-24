import { useEffect } from 'react'
import { catalog } from './catalog.js'

// Planted regressions: a large data module in the chunk, one row per catalog entry,
// a chart image with no alt text that is not in the build, and an uncaught error after
// render. The error is thrown from a timer: thrown inside the effect, React would unmount
// the whole tree.
export default function Reports() {
  useEffect(() => {
    setTimeout(() => {
      throw new Error('reports: chart data missing')
    })
  }, [])
  return (
    <section>
      <h2>Reports</h2>
      <img src="/chart.png" width="400" height="200" />
      <table>
        <tbody>
          {catalog.map((c) => (
            <tr key={c.sku}>
              <td>{c.sku}</td>
              <td>{c.name}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
