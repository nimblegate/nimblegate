<script>
  // Positive: +page.svelte imports a component that uses $env/dynamic/public
  // at module level — when this page imports, the component's module body
  // runs eagerly and the env access can return undefined.
  import Header from './Header.svelte';
</script>

<Header />
<h1>Welcome</h1>
