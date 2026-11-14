package tailwind

// preflight is Tailwind v3's base layer (a modern-normalize derivative),
// plus the defaults of the custom properties utilities compose.
const preflight = `*, ::before, ::after {
  box-sizing: border-box; border-width: 0; border-style: solid; border-color: #e5e7eb;
  --tw-translate-x: 0px; --tw-translate-y: 0px; --tw-rotate: 0deg; --tw-skew-x: 0deg; --tw-skew-y: 0deg;
  --tw-scale-x: 1; --tw-scale-y: 1;
}
html {
  line-height: 1.5; tab-size: 4;
  font-family: ui-sans-serif, system-ui, sans-serif, "Apple Color Emoji", "Segoe UI Emoji", "Segoe UI Symbol", "Noto Color Emoji";
}
body { margin: 0; line-height: inherit; }
hr { height: 0; color: inherit; border-top-width: 1px; }
h1, h2, h3, h4, h5, h6 { font-size: inherit; font-weight: inherit; }
a { color: inherit; text-decoration: inherit; }
b, strong { font-weight: bolder; }
code, kbd, samp, pre {
  font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  font-size: 1em;
}
small { font-size: 80%; }
sub, sup { font-size: 75%; line-height: 0; position: relative; vertical-align: baseline; }
sub { bottom: -0.25em; }
sup { top: -0.5em; }
table { text-indent: 0; border-color: inherit; border-collapse: collapse; }
button, input, optgroup, select, textarea {
  font-family: inherit; font-size: 100%; font-weight: inherit; line-height: inherit;
  letter-spacing: inherit; color: inherit; margin: 0; padding: 0;
}
button, select { text-transform: none; }
button, input[type=button], input[type=reset], input[type=submit] { background-color: transparent; background-image: none; }
summary { display: list-item; }
blockquote, dl, dd, h1, h2, h3, h4, h5, h6, hr, figure, p, pre { margin: 0; }
fieldset { margin: 0; padding: 0; }
legend { padding: 0; }
ol, ul, menu { list-style: none; margin: 0; padding: 0; }
dialog { padding: 0; }
textarea { resize: vertical; }
img, svg, video, canvas, audio, iframe, embed, object { display: block; vertical-align: middle; }
img, video { max-width: 100%; height: auto; }
[hidden] { display: none; }
`
