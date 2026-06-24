<script>
  // Static env — build-time. Safe to use at module scope.
  import { PUBLIC_API_URL } from '$env/static/public';
  const API = PUBLIC_API_URL;
</script>
<header>API: {API}</header>
