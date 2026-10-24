<script>
  const rows = [
    { month: 'January', orders: 12 },
    { month: 'February', orders: 15 },
    { month: 'March', orders: 9 },
  ]
</script>

<section>
  <h2>Reports</h2>
  <table>
    <thead>
      <tr><th>Month</th><th>Orders</th></tr>
    </thead>
    <tbody>
      {#each rows as r}
        <tr><td>{r.month}</td><td>{r.orders}</td></tr>
      {/each}
    </tbody>
  </table>
</section>
