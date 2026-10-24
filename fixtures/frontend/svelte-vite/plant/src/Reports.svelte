<script>
  import { onMount } from 'svelte'
  import { catalog } from './catalog.js'

  // Planted regressions: a large data module in the chunk, one row per catalog entry,
  // a chart image with no alt text that is not in the build, and an error after mount.
  onMount(() => {
    setTimeout(() => {
      throw new Error('reports: chart data missing')
    })
  })
</script>

<section>
  <h2>Reports</h2>
  <img src="/chart.png" width="400" height="200" />
  <table>
    <tbody>
      {#each catalog as c}
        <tr><td>{c.sku}</td><td>{c.name}</td></tr>
      {/each}
    </tbody>
  </table>
</section>
