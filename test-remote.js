import assert, { strictEqual } from "node:assert"
import { describe, it } from "node:test"
import { hexToBytes } from "@helios-lang/codec-utils"
import { Program } from "@helios-lang/compiler"
import {
    addValues,
    makeAddress,
    makeAssetClass,
    makeInlineTxOutputDatum,
    makeMintingPolicyHash,
    makeNetworkParamsHelper,
    makeTxId,
    makeTxInput,
    makeTxOutputId,
    makeValue,
    parseTxOutputId
} from "@helios-lang/ledger"
import { makeIntData } from "@helios-lang/uplc"
import {
    makeIrisClient,
    makeTxBuilder,
    makeTxChainBuilder,
    makeUnstakedSimpleWallet,
    restoreRootPrivateKey
} from "@helios-lang/tx-utils"
import { expectDefined } from "@helios-lang/type-utils"

/**
 * @type {string}
 */
const host = `https://${expectDefined(process.env.HOST, "env var HOST not set")}`

/**
 * @type {string}
 */
const phrase = expectDefined(process.env.WALLET, "env var WALLET not set")

describe("IrisClient", async () => {
    const client = makeIrisClient(host, false)

    await it("get parameters return object with expected fields", async () => {
        const params = await client.parameters

        assert(
            Object.keys(params).length > 5,
            "expected at least 5 entries in network parameters"
        )

        // roundtrip time-to-slot and slot-to-time is the same
        const helper = makeNetworkParamsHelper(params)
        const time = 1750768620000
        strictEqual(helper.slotToTime(helper.timeToSlot(time)), time, "slot-to-time roundtrip not equal")
    })

    await it("getTx() returns a known Tx with a UTXO at an expected addr", async () => {
        const txID =
            "33b2cad72d0f0ddafcfc16fabcf92d2ab0e9d4034ea40ab1bc1f4dfffb15fbc9"

        const tx = await client.getTx(makeTxId(txID))

        const expectedAddr = makeAddress(
            "addr_test1wrnpd4l7jtfs0sgzuks7w0wvxwkcul6uag34xjvhgsxwj2qk5yq82"
        )
        assert(
            tx.body.outputs.some((utxo) => utxo.address.isEqual(expectedAddr)),
            `expected at least 1 addr ${expectedAddr.toString()}`
        )
    })

    await it("getUtxo() returns a known UTXO containing a ref script", async () => {
        const utxoID =
            "33b2cad72d0f0ddafcfc16fabcf92d2ab0e9d4034ea40ab1bc1f4dfffb15fbc9#0"

        const utxo = await client.getUtxo(parseTxOutputId(utxoID))

        const expectedAddr = makeAddress(
            "addr_test1wrnpd4l7jtfs0sgzuks7w0wvxwkcul6uag34xjvhgsxwj2qk5yq82"
        )
        assert(utxo.address.isEqual(expectedAddr), "utxo at unexpected address")
    })

    await it("getUtxo() throws a UtxoAlreadySpentError for a UTXO that is known to be already spent", async () => {
        const utxoID =
            "33b2cad72d0f0ddafcfc16fabcf92d2ab0e9d4034ea40ab1bc1f4dfffb15fbc9#1"

        try {
            await client.getUtxo(parseTxOutputId(utxoID))

            throw new Error("expected utxo to be already spent")
        } catch (e) {
            if ("consumedBy" in e) {
                assert(
                    e.consumedBy.isEqual(
                        makeTxId(
                            "2a710139fe3d83dc16f9dd3e9e267a98a38d8d5c23ab8b4742f0c0cc8a947ef0"
                        )
                    )
                )
            } else {
                throw e
            }
        }
    })

    await it("getUtxos() returns some UTXOs", async () => {
        let addr =
            "addr_test1wq0a8zn7z544qvlxkt69g37thxrg8fepfuat9dcmnla2qjcysrmal"
        addr = "addr_test1wqyp8f3s30t0kvqa3vfgq8lrhv7vtxrn9w9k9vh5s4syzacyjcr9g"

        const utxos = await client.getUtxos(makeAddress(addr))

        assert(utxos.length > 0, "expected more than 0 UTXOs, got 0")
        assert(
            utxos.every((utxo) => utxo.address.isEqual(makeAddress(addr))),
            "some utxos at unexpected address"
        )
    })

    await it("getUtxosWithAssetClass() returns some UTXOs with known assets", async () => {
        const addr =
            "addr_test1wqyp8f3s30t0kvqa3vfgq8lrhv7vtxrn9w9k9vh5s4syzacyjcr9g"
        const asset = makeAssetClass(
            makeMintingPolicyHash(
                "1fd38a7e152b5033e6b2f45447cbb98683a7214f3ab2b71b9ffaa04b"
            ),
            hexToBytes("7450424720737570706c79")
        )

        const utxos = await client.getUtxosWithAssetClass(
            makeAddress(addr),
            asset
        )
        strictEqual(
            utxos.length,
            1,
            "unexpected number of utxos at address " + addr
        )
        assert(
            utxos.every(
                (utxo) =>
                    utxo.address.isEqual(makeAddress(addr)) &&
                    utxo.value.assets.getAssetClassQuantity(asset) == 1n
            ),
            "some utxos at unexpected address without asset class"
        )
    })

    await it("getAddressesWithAssetClass() returns a single address for a known NFT", async () => {
        const asset = makeAssetClass(
            makeMintingPolicyHash(
                "1fd38a7e152b5033e6b2f45447cbb98683a7214f3ab2b71b9ffaa04b"
            ),
            hexToBytes("7450424720737570706c79")
        )

        const addresses = await client.getAddressesWithAssetClass(asset)

        strictEqual(addresses.length, 1, "expected exactly a single address")

        assert(
            addresses.every((addr) =>
                addr.address.isEqual(
                    makeAddress(
                        "addr_test1wqyp8f3s30t0kvqa3vfgq8lrhv7vtxrn9w9k9vh5s4syzacyjcr9g"
                    )
                )
            )
        )
    })

    if (phrase != "") {
        const key = restoreRootPrivateKey(phrase.split(" "))

        // | n   | blockfrost | iris |
        // | 10  | ok         | ok   |
        // | 25  | ok         | ok   |
        // | 50  | ok         | ok   |
        // | 100 | ok         | ok   |
        // | 500 | ok         | ok   |
        const n = 10
        await it(`tx chain with ${n} txs`, async () => {
            
            const unchainedWallet = makeUnstakedSimpleWallet(key, client)
            console.log(`Test wallet address: ${unchainedWallet.address.toString()}`)

            // the first tx creates a ref input for the latter
            const tx0 = await makeTxBuilder({ isMainnet: false })
                .spendUnsafe(await unchainedWallet.utxos)
                .payUnsafe(
                    unchainedWallet.address,
                    makeValue(5_000_000n),
                    makeInlineTxOutputDatum(makeIntData(0))
                )
                .build({
                    changeAddress: unchainedWallet.address
                })

            const refID = makeTxOutputId(tx0.id(), 0)
            const ref = makeTxInput(refID, tx0.body.outputs[0])

            tx0.addSignatures(await unchainedWallet.signTx(tx0))
            console.log("submitting tx0")
            const id0 = await unchainedWallet.submitTx(tx0)

            console.log("submitted tx0 " + id0.toString())

            // each tx pays everything to itself
            const chain = makeTxChainBuilder(client)
            const wallet = makeUnstakedSimpleWallet(key, chain)

            for (let i = 1; i < n; i++) {
                // This now relies on the mempool overlay
                const allUtxos = (await wallet.utxos).filter(
                    (utxo) => !utxo.id.isEqual(refID)
                )

                console.log(
                    `Spending ${allUtxos.map((utxo) => utxo.id.toString())}`
                )
                const tx = await makeTxBuilder({ isMainnet: false })
                    .refer(ref)
                    .spendUnsafe(allUtxos)
                    .payUnsafe(
                        wallet.address,
                        addValues(allUtxos).subtract(makeValue(5_000_000n))
                    )
                    .build({
                        changeAddress: wallet.address
                    })

                tx.addSignatures(await wallet.signTx(tx))
                await chain.submitTx(tx)
            }

            const txs = chain.build()

            console.log(`submitting ${n} txs`)
            for (let tx of txs.txs) {
                const id = await client.submitTx(tx)

                console.log("submitted " + id.toString())

                await client.getTx(id)

                console.log("  and fetched immediately after")

                await client.getUtxo(makeTxOutputId(id, 0))

                console.log("  and first UTXO fetched immediately after")

                // wait some time between submission, in order to test the node memory
                await new Promise((resolve) => setTimeout(resolve, 5000))
            }
        })

        // TODO: a special test, that checks that an on-chain datum contains the off-chain time range start
        await it("converts slot to time correctly", async () => {
            const validator = new Program(`minting time_range_start
            import { tx } from ScriptContext    
            func main(r: Int) -> Bool {
                Time::new(r) == tx.time_range.start
            }`)
            const params = await client.parameters

            const uplc = validator.compile({optimize: false})
            const mph = makeMintingPolicyHash(uplc.hash())

            const wallet = makeUnstakedSimpleWallet(key, client)
            const t = (Math.round(Date.now()/1000) - 90)*1000
            const tx = await makeTxBuilder({isMainnet: false})
                .attachUplcProgram(uplc)
                .mintAssetClassUnsafe(makeAssetClass(mph, []), 1, makeIntData(t))
                .validFromTime(t)
                .build({
                    changeAddress: wallet.address,
                    spareUtxos: wallet.utxos,
                    networkParams: params
                })

            console.log(`ref slot: ${params.refTipSlot}, ref time: ${params.refTipTime}`)
            console.log(`tx start slot: ${tx.body.firstValidSlot}, tx start time: ${t}`)
            tx.addSignatures(await wallet.signTx(tx))

            try {
                await client.submitTx(tx)
            }catch(e) {
                console.log(`failed to submit ${JSON.stringify(tx.dump())}`)
                throw e
            }
        })
        
    }
})
