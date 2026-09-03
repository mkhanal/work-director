// Flat-config fragment: import and spread into your eslint.config.js `rules`.
// Requires typescript-eslint with type-checked parser options for the @typescript-eslint rules.
import rules from './rules.json' with { type: 'json' };
export default { rules };
