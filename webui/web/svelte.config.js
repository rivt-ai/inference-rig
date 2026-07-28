// Opt into runes mode explicitly. Svelte 5 otherwise infers the mode per
// component, so adding the first rune to a component silently converts its
// plain `let` declarations into non-reactive locals — the page stops updating
// with no error. Committing to runes keeps that failure impossible.
export default { compilerOptions: { runes: true } };
