# Code-span negative

Links inside code are illustrative, not real links - per CommonMark they are
not parsed. None of the following should be flagged as broken.

Inline code: users can type `[link](url)` or `[a](./does-not-exist.md)` in a field.

A fenced example:

```
See [x](./also-missing.md) and the image ![img](/img.jpg)
[click](javascript:alert(1))
```

End of document.
