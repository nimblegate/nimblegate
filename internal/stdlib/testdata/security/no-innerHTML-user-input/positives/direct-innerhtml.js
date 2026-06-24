// Positive: user input flows directly to .innerHTML without sanitization.
// Classic XSS vector.
const userBio = req.body.bio;
document.getElementById("profile").innerHTML = userBio;
