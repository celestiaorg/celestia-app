# Celestia App Specifications

## Building From Source

Install [mdbook](https://rust-lang.github.io/mdBook/guide/installation.html) and [mdbook-toc](https://github.com/badboy/mdbook-toc):

```sh
cargo install mdbook
cargo install mdbook-toc
```

To build book:

```sh
mdbook build
```

To serve locally:

```sh
mdbook serve
```

## Contributing

Markdown files must conform to [GitHub Flavored Markdown](https://github.github.com/gfm/). Markdown must be formatted with:

- [markdownlint](https://github.com/DavidAnson/markdownlint)
- [Markdown Table Prettifier](https://github.com/darkriszty/MarkdownTablePrettify-VSCodeExt)
