<script>
  const products = ['Kettle', 'Teapot', 'Mug', 'Strainer', 'Serving tray']
  let reports = $state(null)

  // Reports is a separate chunk, so the fixture exercises Vite's preload code.
  async function showReports() {
    reports = (await import('./Reports.svelte')).default
  }
</script>

<main>
  <header>
    <img src="/logo.svg" alt="Fixture shop logo" width="32" height="32" />
    <h1>Fixture shop</h1>
  </header>
  <ul id="products">
    {#each products as p}
      <li>{p}</li>
    {/each}
  </ul>
  <button id="show-reports" type="button" onclick={showReports}>Show sales reports</button>
  {#if reports}
    {@const Reports = reports}
    <Reports />
  {/if}
</main>
