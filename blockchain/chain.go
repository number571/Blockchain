package blockchain

import (
    "os"
    "fmt"
    "bytes"
    "errors"
    "database/sql"
    _ "github.com/mattn/go-sqlite3"
)

func NewChain(filename, receiver string) error {
    file, err := os.Create(filename)
    if err != nil {
        return errors.New("create database")
    }
    file.Close()
    db, err := sql.Open("sqlite3", filename)
    if err != nil {
        return errors.New("open database")
    }
    defer db.Close()
    _, err = db.Exec(`
CREATE TABLE BlockChain (
    Id INTEGER PRIMARY KEY AUTOINCREMENT,
    Hash VARCHAR(44) UNIQUE,
    Block TEXT
);
`)
    chain := &BlockChain{
        DB: db,
    }
    genesis := &Block{
        CurrHash: []byte(GENESIS_BLOCK),
        Mapping: make(map[string]uint64),
    }
    genesis.Mapping[STORAGE_CHAIN] = STORAGE_VALUE
    genesis.Mapping[receiver] = GENESIS_REWARD
    chain.AddBlock(genesis)
    return nil
}

func LoadChain(filename string) *BlockChain {
    db, err := sql.Open("sqlite3", filename)
    if err != nil {
        return nil 
    }
    chain := &BlockChain{
        DB: db,
    }
    chain.Index = chain.Size()
    return chain
}

func (chain *BlockChain) Size() uint64 {
    var index uint64
    row := chain.DB.QueryRow("SELECT Id FROM BlockChain ORDER BY Id DESC")
    row.Scan(&index)
    return index
}

func (chain *BlockChain) PrintChain() error {
    rows, err := chain.DB.Query("SELECT Id, Block FROM BlockChain")
    if err != nil {
        return err
    }
    defer rows.Close()
    var (
        sblock string
        block  *Block
        index  uint64
        size   uint64
    )
    for rows.Next() {
        rows.Scan(&index, &sblock)
        block = DeserializeBlock(sblock)

        if index == 1 {
            if !bytes.Equal(block.CurrHash, []byte(GENESIS_BLOCK)) {
                fmt.Printf("[%d][FAILED] Genesis block undefined\n", index)
            } else {
                fmt.Printf("[%d][SUCCESS] Genesis block found\n", index)
            }
            goto print
        }

        if block.Difficulty != DIFFICULTY {
            fmt.Printf("[%d][FAILED] difficulty is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] difficulty is valid\n", index)
        }

        if !block.hashIsValid() {
            fmt.Printf("[%d][FAILED] hash is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] hash is valid\n", index)
        }

        if !block.signIsValid() {
            fmt.Printf("[%d][FAILED] sign is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] sign is valid\n", index)
        }

        if !block.proofIsValid() {
            fmt.Printf("[%d][FAILED] proof is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] proof is valid\n", index)
        }

        if !block.mappingIsValid() {
            fmt.Printf("[%d][FAILED] mapping is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] mapping is valid\n", index)
        }

        size = chain.Index
        chain.Index = index - 1
        if !chain.transactionsIsValid(block) {
            fmt.Printf("[%d][FAILED] transactions is not valid\n", index)
        } else {
            fmt.Printf("[%d][SUCCESS] transactions is valid\n", index)
        }
        chain.Index = size

print:
        fmt.Printf("[%d] => %s\n\n", index, sblock)
    }
    return nil
}

func (chain *BlockChain) Balance(address string) uint64 {
    var (
        sblock string
        block *Block
        balance uint64
    )
    rows, err := chain.DB.Query("SELECT Block FROM BlockChain WHERE Id <= $1 ORDER BY Id DESC", chain.Index)
    if err != nil {
        return balance
    }
    defer rows.Close()
    for rows.Next() {
        rows.Scan(&sblock)
        block = DeserializeBlock(sblock)
        if value, ok := block.Mapping[address]; ok {
            balance = value
            break
        }
    }
    return balance
}

func (chain *BlockChain) LastHash() []byte {
    var hash string
    row := chain.DB.QueryRow("SELECT Hash FROM BlockChain ORDER BY Id DESC")
    row.Scan(&hash)
    return Base64Decode(hash)
}

func (chain *BlockChain) PushBlock(user *User, block *Block) {
    if !chain.transactionsIsValid(block) {
        return
    }
    block.AddTransaction(chain, &Transaction{
        RandBytes: GenerateRandomBytes(RAND_BYTES),
        Sender: STORAGE_CHAIN,
        Receiver: user.Address(),
        Value: STORAGE_REWARD,
    })
    block.CurrHash  = block.hash()
    block.Signature = block.sign(user.Private())
    block.Nonce     = block.proof()
    chain.AddBlock(block)
}

func (chain *BlockChain) AddBlock(block *Block) {
    chain.Index += 1
    chain.DB.Exec("INSERT INTO BlockChain (Hash, Block) VALUES ($1, $2)", 
        Base64Encode(block.CurrHash),
        SerializeBlock(block),
    )
}

func (chain *BlockChain) transactionsIsValid(block *Block) bool {
    lentxs := len(block.Transactions)
    plusStorage := 0
    for i := 0; i < lentxs; i++ {
        if block.Transactions[i].Sender == STORAGE_CHAIN {
            plusStorage = 1
            break
        }
    }
    if lentxs == 0 || lentxs > TXS_LIMIT + plusStorage {
        return false
    }
    for i := 0; i < lentxs-1; i++ {
        for j := i+1; j < lentxs; j++ {
            // rand bytes not be equal
            if bytes.Equal(block.Transactions[i].RandBytes, block.Transactions[j].RandBytes) {
                return false
            }
            // storage tx only one
            if block.Transactions[i].Sender == STORAGE_CHAIN && block.Transactions[j].Sender == STORAGE_CHAIN {
                return false
            }
        }
    }
    for i := 0; i < lentxs; i++ {
        tx := block.Transactions[i]
        // storage tx has no hash and signature
        if tx.Sender == STORAGE_CHAIN {
            if tx.Receiver != block.Miner || tx.Value != STORAGE_REWARD {
                return false
            }
        } else {
            if !tx.hashIsValid() {
                return false
            }
            if !tx.signIsValid() {
                return false
            }
        }
        if !chain.balanceIsValid(block, tx.Sender) {
            return false
        }
        if !chain.balanceIsValid(block, tx.Receiver) {
            return false
        }
    }
    return true
}

func (chain *BlockChain) balanceIsValid(block *Block, address string) bool {
    lentxs := len(block.Transactions)
    balanceInChain := chain.Balance(address)
    balanceSubBlock := uint64(0)
    balanceAddBlock := uint64(0)
    for j := 0; j < lentxs; j++ {
        tx := block.Transactions[j]
        if tx.Sender == address {
            balanceSubBlock += tx.Value + tx.ToStorage
        }
        if tx.Receiver == address {
            balanceAddBlock += tx.Value
        }
        if STORAGE_CHAIN == address {
            balanceAddBlock += tx.ToStorage
        }
    }
    if _, ok := block.Mapping[address]; !ok {
        return false
    }
    if (balanceInChain + balanceAddBlock - balanceSubBlock) != block.Mapping[address] {
        return false
    }
    return true
}