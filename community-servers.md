# Community-run `dename` servers

### `mit-pilot`

The pilot server, run by the people who started the project.  Used by default
when no configuration file is provided. Should probably accept signatures from
all servers listed [here](github.com/andres-erbsen/dename/tree/master/community-servers.md).
Currently the only server that can handle updates.

- Accepts signatures: `dename.alokat.org`
- Requires signatures: (none)
- Contact: PGP: `CFCA 4540 99B1 6042 F832 A708 4A33 C134 D6C4 7A84`, `dename`: `andres`
- Config entries:

		[verifier "mit-pilot"]
		PublicKey = CiCheFqDmJ0Pg+j+lypkmmiHrFmRn50rlDi5X0l4+lJRFA==
		[update "dename.mit.edu:6263"]
		TransportPublicKey = 4f2i+j65JCE2xNKhxE3RPurAYALx9GRy0Pm9c6J7eDY=
		[lookup "dename.mit.edu:6263"]
		TransportPublicKey = 4f2i+j65JCE2xNKhxE3RPurAYALx9GRy0Pm9c6J7eDY=

### `dename.alokat.org`

- Accepts signatures: `dename.mit.edu`
- Requires signatures: `dename.mit.edu`
- Contact: PGP: `4981 E0FD 7206 B64A 0384 7B1A 1E1D 9C86 A5BE 1A64`, `dename`: `fritjof`
- Config entries:

		[verifier "alokat"]
		PublicKey = CiD6CFKBpG54dG3OMx6PJ58z5rlNFK24Dx2HMpR7urHIVA==
		[lookup "dename.alokat.org:6263"]
		TransportPublicKey = IoEJsVcspYNiuymi+JMpfkL1usDy482qE8V4aGvKrkY=

### `dename02.alokat.org`

This server has a dynamic ip, but will be updated as soon as the ip changes. 

- Accepts signatures: `dename.mit.edu`
- Requires signatures: `dename.mit.edu`
- Contact: PGP: `4981 E0FD 7206 B64A 0384 7B1A 1E1D 9C86 A5BE 1A64`, `dename`: `fritjof`
- Config entries:

		[verifier "alokat-02"]
		PublicKey = CiDCighTMdpZtTDNyKNSQ94Y7vtzEDATDsWCBH1kK7wF4Q==
		[lookup "dename02.alokat.org:6263"]
		TransportPublicKey = 1gTqwbutP0yG6yL+1mAXK+sCMRaBeNqbuKgTVIkDgDY=

