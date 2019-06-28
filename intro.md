# Introducing dcrpool.

Decred's high [network security](https://medium.com/decred/decreds-hybrid-protocol-a-superior-deterrent-to-majority-attacks-9421bf486292) is a result of its hybrid Proof-of-Work (PoW) and Proof-of-Stake (PoS) mining system. It depends on how decentralized Decred's PoW and PoS actors are. With over twenty (20) [Voting Service Providers](https://decred.org/vsp/) (VSP) and open-source [VSP software](https://github.com/decred/dcrstakepool), the PoS aspect of the network is currently the most decentralized. 

Since the introduction of Application-Specific Integrated Circuit (ASIC) miners to the network, people looking to become PoW miners have had limited options of either joining an existing mining pool or writing pool mining software themselves. For most, the latter is not viable. It is no surprise most miners prefer to join an existing mining pool, but unfortunately this could result in hash power centralization for the network.

This problem is not unique to Decred, high quality open source mining pool software is rare in the cryptocurrency world. Miners and mining pools are in competition with each other, the need for usable software acts as a barrier to entry for new pools and better software may give some pools a competitive edge.

Ideally, having a solo pool mining setup for each PoW miner would be the most secure and decentralized setup. This is unrealistic however for reasons of cost. The more realistic scenario is to have mining pools serving PoW miners with small hash power and provide open-source mining pool software that PoW miners with large hash power can use for their solo pools. The availability of open-source mining pool software would also allow community members to setup more mining pools, which provides more choice and fosters healthy competition among pool operators.

We've been working on this open-source mining pool for the Decred network for a while now. After many months of development I'm happy to introduce [dcrpool](https://github.com/decred/dcrpool), an open-source stratum mining pool.

Dcrpool currently supports Innosilicon D9, Antminer DR3, Antminer DR5 and Whatsminer D1 ASIC miners. The pool can be configured to mine in solo pool mode or as a publicly available mining pool. It supports both Pay Per Share (PPS) and Pay Per Last N Shares (PPLNS) payment schemes when configured as a mining pool. When configured as a solo pool, mining rewards are left to accrue at the mining address set for the node. The pool provides a user interface for pool statistics, connection details for all supported miners, account work and payment analysis, and pool database backup reserved for the pool administrator.

![dcrpool frontend](https://i.ibb.co/zfKCfyn/dcrpool.png)

Dcrpool strives to be as transparent as possible by allowing users of pool accounts to access a detailed breakdown of the blocks they have mined for the pool, the payments made by the pool to their address and the current work quotas for the next block to be mined by the pool. 

With the release of dcrpool we hope PoW miners that would benefit from running their own private mining operation proceed to do so. We also hope more mining pools will be created by interested community members and that this will increase the options available to miners.

Tell a friend to tell a friend, happy mining :)



