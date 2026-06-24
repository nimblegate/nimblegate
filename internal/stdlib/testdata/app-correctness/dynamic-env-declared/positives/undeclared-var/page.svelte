<script>
  import { env } from '$env/dynamic/public';
  // Positive: PUBLIC_UNDECLARED_API is referenced here but the paired
  // wrangler.toml doesn't list it under [vars] — runtime undefined risk.
  const apiUrl = env.PUBLIC_UNDECLARED_API;
  console.log(apiUrl);
</script>
