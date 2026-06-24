<script>
  // Negative: +page.svelte imports a component that uses $env/static/public
  // — build-time inlined, no runtime undefined risk.
  import Header from './Header.svelte';
</script>

<Header />
<h1>Welcome</h1>
