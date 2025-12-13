# Configuration file for the Sphinx documentation builder.
#
# For the full list of built-in configuration values, see the documentation:
# https://www.sphinx-doc.org/en/master/usage/configuration.html

# -- Path setup --------------------------------------------------------------

import subprocess
from datetime import datetime

# -- Project information -----------------------------------------------------

project = 'godi'
author = 'junioryono'
copyright = f'{datetime.now().year}, {author}'

# Read version from git tags
def get_version():
    try:
        result = subprocess.run(
            ['git', 'describe', '--tags', '--abbrev=0'],
            capture_output=True,
            text=True,
            cwd='..'
        )
        if result.returncode == 0:
            return result.stdout.strip()
    except Exception:
        pass
    return 'v0.0.0'

# The full version, including alpha/beta/rc tags
release = get_version()
version = release

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
    "installation": "getting-started/01-installation",
    "core-concepts": "concepts/how-it-works",
    "service-lifetimes": "concepts/lifetimes",
    "scopes-isolation": "concepts/scopes",
    "modules": "concepts/modules",
    "keyed-services": "features/keyed-services",
    "service-groups": "features/service-groups",
    "parameter-objects": "features/parameter-objects",
    "result-objects": "features/result-objects",
    "interface-registration": "features/interface-binding",
    "resource-management": "features/resource-cleanup",
    "dependency-resolution": "concepts/how-it-works",
    "service-registration": "getting-started/03-adding-services",
}