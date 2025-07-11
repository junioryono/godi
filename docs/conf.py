# Configuration file for the Sphinx documentation builder.
#
# For the full list of built-in configuration values, see the documentation:
# https://www.sphinx-doc.org/en/master/usage/configuration.html

# -- Path setup --------------------------------------------------------------

import os
import sphinx_rtd_theme

# -- Project information -----------------------------------------------------

project = 'godi'
copyright = '2025, junioryono'
author = 'junioryono'

# The full version, including alpha/beta/rc tags
release = 'v1.0.0'

# -- General configuration ---------------------------------------------------

# Add any Sphinx extension module names here, as strings.
extensions = [
    'myst_parser',
    'sphinx_rtd_theme',
    'sphinx_favicon',
    'sphinxext.rediraffe',
    'sphinx_copybutton',
]

# Add any paths that contain templates here, relative to this directory.
templates_path = ['_templates']

# List of patterns, relative to source directory, that match files and
# directories to ignore when looking for source files.
exclude_patterns = ['_build', '_venv', 'Thumbs.db', '.DS_Store']

# -- Options for HTML output -------------------------------------------------

# The theme to use for HTML and HTML Help pages.
html_theme = 'sphinx_rtd_theme'

# Add any paths that contain custom static files (such as style sheets) here,
# relative to this directory.
html_static_path = ['_static']

html_logo = "_static/logo.png"
html_theme_options = {
    'logo_only': True,
    'display_version': True,
    'prev_next_buttons_location': 'both',
    'style_external_links': False,
    'style_nav_header_background': '#2980B9',
    # Toc options
    'collapse_navigation': False,
    'sticky_navigation': True,
    'navigation_depth': 4,
    'includehidden': True,
    'titles_only': False
}

html_context = {
    'display_github': True,
    'github_user': 'junioryono',
    'github_repo': 'godi',
    'github_version': 'main',
    'conf_py_path': '/docs/',
}

def setup(app):
    app.add_css_file('customize.css')

# Favicons
favicons = [
    "favicon.png",
]

# MyST parser configuration
myst_enable_extensions = [
    "attrs_inline",
    "colon_fence",
    "deflist",
    "tasklist",
]

# Copy button configuration
copybutton_prompt_text = r"$ |>>> |\.\.\. "
copybutton_prompt_is_regexp = True

# Rediraffe configuration for redirects
rediraffe_redirects = {
    # Add any page redirects here
}