# Upgrades

This directory contains one directory per app version upgrade. For example the upgrade logic involved in upgrading from app version 1 => 2 is contained in the `v2` directory. Celestia app doesn't use the Cosmos SDK x/upgrade module to manage upgrades. Instead it uses a versioned module manager and configurator. See `app/module` for details.
