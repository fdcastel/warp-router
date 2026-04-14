# General

- Project tasks are in `doc/WARP_ROUTER_PLAN.md`. Keep this file always updated. 
- Project decisions and lessons learned are kept in `doc/WARP_ROUTER_DECISIONS.md`. Keep this file always updated. 
- Do smaller commits. Use conventional commits.
- Use `./tmp` folder for temporary files and scripts. Do not pollute the project folder.

# About tests

- Not tested = not working.
- Task is only really done when tested. 
- Tests must prove the code written does what was intended.
- NO MOCKS FOR TESTING. Tests must make the code meet real world scenarios.
- See `tmp/TEST_SERVER.md` for details about how to access the Proxmox VE test server.
