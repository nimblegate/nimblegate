---
id: protocol-downgrade-on-secure-context
description: Insecure resource referenced from a secure (https) page, breaking the secure context guarantees.
anticipated-siblings: []
---

# Pattern: protocol-downgrade-on-secure-context

An https page loads `http://...` resources. Browsers either block them outright (modern) or warn the user (older). Either way, the secure-context guarantee is broken: a network attacker can rewrite the http response, inject scripts, exfiltrate cookies. The page authored the downgrade by linking to the insecure URL.

The structural defense: scan committed HTML and templates for http://-prefixed URLs in `src=` / `href=` / `srcset=` attributes. Exempt the documented cases (xmlns, intentional localhost, RFC1918 private ranges). Refuse the rest at commit time. The fix is usually a one-character edit (s/http/https/); the saved bug is a class of mixed-content vulnerabilities.
