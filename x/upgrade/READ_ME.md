# `x/upgrade`

## Abstract

The upgrade module removes the entrypoints to the standard upgrade module by not
registering a message server. It registers the standard upgrade module types to
preserve the ability to marshal them. Note that the keeper of the standard
upgrade module is still added to the application.
