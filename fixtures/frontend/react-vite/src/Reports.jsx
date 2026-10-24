const rows = [
  { month: 'January', orders: 12 },
  { month: 'February', orders: 15 },
  { month: 'March', orders: 9 },
]

export default function Reports() {
  return (
    <section>
      <h2>Reports</h2>
      <table>
        <thead>
          <tr>
            <th>Month</th>
            <th>Orders</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.month}>
              <td>{r.month}</td>
              <td>{r.orders}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </section>
  )
}
