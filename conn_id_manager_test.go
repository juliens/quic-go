package quic

import (
	"github.com/lucas-clemente/quic-go/internal/protocol"
	"github.com/lucas-clemente/quic-go/internal/wire"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Connection ID Manager", func() {
	var (
		m          *connIDManager
		frameQueue []wire.Frame
	)

	BeforeEach(func() {
		frameQueue = nil
		m = newConnIDManager(func(f wire.Frame) {
			frameQueue = append(frameQueue, f)
		})
	})

	getNext := func() (protocol.ConnectionID, *[16]byte) {
		if m.queue.Len() == 0 {
			return nil, nil
		}
		val := m.queue.Remove(m.queue.Front())
		return val.ConnectionID, &val.StatelessResetToken
	}

	It("returns nil if empty", func() {
		c, rt := getNext()
		Expect(c).To(BeNil())
		Expect(rt).To(BeNil())
	})

	It("adds and gets connection IDs", func() {
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber:      10,
			ConnectionID:        protocol.ConnectionID{2, 3, 4, 5},
			StatelessResetToken: [16]byte{0xe, 0xd, 0xc, 0xb, 0xa, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
		})).To(Succeed())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber:      4,
			ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
			StatelessResetToken: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe},
		})).To(Succeed())
		c1, rt1 := getNext()
		Expect(c1).To(Equal(protocol.ConnectionID{1, 2, 3, 4}))
		Expect(*rt1).To(Equal([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe}))
		c2, rt2 := getNext()
		Expect(c2).To(Equal(protocol.ConnectionID{2, 3, 4, 5}))
		Expect(*rt2).To(Equal([16]byte{0xe, 0xd, 0xc, 0xb, 0xa, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0}))
		c3, rt3 := getNext()
		Expect(c3).To(BeNil())
		Expect(rt3).To(BeNil())
	})

	It("accepts duplicates", func() {
		f := &wire.NewConnectionIDFrame{
			SequenceNumber:      1,
			ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
			StatelessResetToken: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe},
		}
		Expect(m.Add(f)).To(Succeed())
		Expect(m.Add(f)).To(Succeed())
		c1, rt1 := getNext()
		Expect(c1).To(Equal(protocol.ConnectionID{1, 2, 3, 4}))
		Expect(*rt1).To(Equal([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe}))
		c2, rt2 := getNext()
		Expect(c2).To(BeNil())
		Expect(rt2).To(BeNil())
	})

	It("rejects duplicates with different connection IDs", func() {
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber: 42,
			ConnectionID:   protocol.ConnectionID{1, 2, 3, 4},
		})).To(Succeed())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber: 42,
			ConnectionID:   protocol.ConnectionID{2, 3, 4, 5},
		})).To(MatchError("received conflicting connection IDs for sequence number 42"))
	})

	It("rejects duplicates with different connection IDs", func() {
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber:      42,
			ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
			StatelessResetToken: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe},
		})).To(Succeed())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber:      42,
			ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
			StatelessResetToken: [16]byte{0xe, 0xd, 0xc, 0xb, 0xa, 9, 8, 7, 6, 5, 4, 3, 2, 1, 0},
		})).To(MatchError("received conflicting stateless reset tokens for sequence number 42"))
	})

	It("retires connection IDs", func() {
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber: 10,
			ConnectionID:   protocol.ConnectionID{1, 2, 3, 4},
		})).To(Succeed())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber: 13,
			ConnectionID:   protocol.ConnectionID{2, 3, 4, 5},
		})).To(Succeed())
		Expect(frameQueue).To(BeEmpty())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			RetirePriorTo:  11,
			SequenceNumber: 17,
			ConnectionID:   protocol.ConnectionID{3, 4, 5, 6},
		})).To(Succeed())
		Expect(frameQueue).To(HaveLen(1))
		Expect(frameQueue[0].(*wire.RetireConnectionIDFrame).SequenceNumber).To(BeEquivalentTo(10))
		c, _ := getNext()
		Expect(c).To(Equal(protocol.ConnectionID{2, 3, 4, 5}))
		c, _ = getNext()
		Expect(c).To(Equal(protocol.ConnectionID{3, 4, 5, 6}))
	})

	It("retires old connection IDs when the peer sends too many new ones", func() {
		for i := uint8(0); i < protocol.MaxActiveConnectionIDs; i++ {
			Expect(m.Add(&wire.NewConnectionIDFrame{
				SequenceNumber: uint64(i),
				ConnectionID:   protocol.ConnectionID{i, i, i, i},
			})).To(Succeed())
		}
		Expect(frameQueue).To(BeEmpty())
		Expect(m.Add(&wire.NewConnectionIDFrame{
			SequenceNumber: protocol.MaxActiveConnectionIDs,
			ConnectionID:   protocol.ConnectionID{1, 2, 3, 4},
		})).To(Succeed())
		Expect(frameQueue).To(HaveLen(1))
		Expect(frameQueue[0].(*wire.RetireConnectionIDFrame).SequenceNumber).To(BeEquivalentTo(0))
	})

	Context("getting new connection IDs", func() {
		It("changes the connection ID as early as possible", func() {
			cid, _ := m.MaybeGetNewConnID()
			Expect(cid).To(BeNil())
			Expect(m.Add(&wire.NewConnectionIDFrame{
				SequenceNumber:      1,
				ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
				StatelessResetToken: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe},
			})).To(Succeed())
			cid, token := m.MaybeGetNewConnID()
			Expect(cid).To(Equal(protocol.ConnectionID{1, 2, 3, 4}))
			Expect(*token).To(Equal([16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe}))
		})

		It("waits until sending some packets and receiving connection IDs after the first change", func() {
			Expect(m.Add(&wire.NewConnectionIDFrame{
				SequenceNumber:      1,
				ConnectionID:        protocol.ConnectionID{1, 2, 3, 4},
				StatelessResetToken: [16]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 0xa, 0xb, 0xc, 0xd, 0xe},
			})).To(Succeed())
			cid, _ := m.MaybeGetNewConnID()
			Expect(cid).ToNot(BeNil())
			for i := uint8(0); i < protocol.MaxActiveConnectionIDs+1; i++ {
				Expect(m.Add(&wire.NewConnectionIDFrame{
					SequenceNumber: uint64(i + 2),
					ConnectionID:   protocol.ConnectionID{i + 2, i + 2, i + 2, i + 2},
				})).To(Succeed())
			}
			cid, _ = m.MaybeGetNewConnID()
			Expect(cid).To(BeNil())
			for i := 0; i < protocol.PacketsPerConnectionID; i++ {
				m.SentPacket()
				cid, _ := m.MaybeGetNewConnID()
				Expect(cid).To(BeNil())
			}
			m.SentPacket()
			cid, _ = m.MaybeGetNewConnID()
			Expect(cid).To(Equal(protocol.ConnectionID{3, 3, 3, 3}))
		})
	})
})
