<script>
  // This component uses $env/dynamic/public at module level. When
  // +page.svelte imports this, the env access runs at import time.
  import { env } from '$env/dynamic/public';
  const API = env.PUBLIC_API_URL;
  console.log("Header loaded with API:", API);
</script>
<header>API: {API}</header>
