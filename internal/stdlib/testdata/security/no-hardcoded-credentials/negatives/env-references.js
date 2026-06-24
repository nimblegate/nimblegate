// Negative: credentials sourced from environment, no hardcoded shape.
const awsKey = process.env.AWS_ACCESS_KEY_ID;
const token = process.env.GITHUB_TOKEN;
const stripe = process.env.STRIPE_SECRET_KEY;
